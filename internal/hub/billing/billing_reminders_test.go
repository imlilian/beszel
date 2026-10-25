//go:build testing

package billing_test

import (
	"sync"
	"testing"
	"time"

	"github.com/henrygd/beszel/internal/alerts"
	"github.com/henrygd/beszel/internal/hub/billing"
	"github.com/henrygd/beszel/internal/tests"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingNotifier captures what the reminder job would deliver instead of
// going through the user's email / webhook settings.
type recordingNotifier struct {
	mu       sync.Mutex
	messages []alerts.AlertMessageData
}

func (r *recordingNotifier) SendAlert(data alerts.AlertMessageData) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, data)
	return nil
}

func (r *recordingNotifier) take() []alerts.AlertMessageData {
	r.mu.Lock()
	defer r.mu.Unlock()
	messages := r.messages
	r.messages = nil
	return messages
}

func testLink(parts ...string) string {
	link := "http://hub.test"
	for _, part := range parts {
		link += "/" + part
	}
	return link
}

type reminderFixture struct {
	hub      *tests.TestHub
	user     *core.Record
	notifier *recordingNotifier
	manager  *billing.Manager
}

func newReminderFixture(t *testing.T) *reminderFixture {
	t.Helper()
	hub, user := tests.GetHubWithUser(t)
	t.Cleanup(hub.Cleanup)
	notifier := &recordingNotifier{}
	return &reminderFixture{
		hub:      hub,
		user:     user,
		notifier: notifier,
		manager:  billing.NewManager(hub, notifier, testLink),
	}
}

// system creates a system owned by users and a paid monthly config expiring in
// daysLeft days. Billing records are written through Save, so the API defaults
// are set explicitly here.
func (f *reminderFixture) system(t *testing.T, name string, users []string) *core.Record {
	t.Helper()
	system, err := tests.CreateRecord(f.hub, "systems", map[string]any{
		"name":  name,
		"host":  "127.0.0.1",
		"port":  "33914",
		"users": users,
	})
	require.NoError(t, err)
	return system
}

func (f *reminderFixture) billingRecord(t *testing.T, systemID string, daysLeft int) *core.Record {
	t.Helper()
	record, err := tests.CreateRecord(f.hub, "system_billing", map[string]any{
		"system":           systemID,
		"billingType":      billing.BillingTypePaid,
		"price":            25,
		"currency":         string(billing.CurrencyCNY),
		"cycle":            string(billing.CycleMonthly),
		"notifyDaysBefore": billing.DefaultNotifyDaysBefore,
		"expiresAt":        time.Now().AddDate(0, 0, daysLeft),
	})
	require.NoError(t, err)
	return record
}

// subscribe turns the BillingExpiry switch on for one user and system.
func (f *reminderFixture) subscribe(t *testing.T, systemID, userID string) *core.Record {
	t.Helper()
	alert, err := tests.CreateRecord(f.hub, "alerts", map[string]any{
		"system": systemID,
		"user":   userID,
		"name":   billing.BillingExpiryAlertName,
		"value":  0,
		"min":    0,
	})
	require.NoError(t, err)
	return alert
}

func (f *reminderFixture) run(t *testing.T) []alerts.AlertMessageData {
	t.Helper()
	require.NoError(t, f.manager.CheckExpiryReminders())
	return f.notifier.take()
}

// remindLevel reads the persisted ladder position back from the database.
func (f *reminderFixture) remindLevel(t *testing.T, billingID string) int {
	t.Helper()
	record, err := f.hub.FindRecordById("system_billing", billingID)
	require.NoError(t, err)
	return record.GetInt("remindLevel")
}

// moveExpiry simulates time passing by rewriting the expiry date. It goes
// through Save rather than the update API, so remindLevel is preserved — which
// is exactly what a later day looks like to the cron job.
func (f *reminderFixture) moveExpiry(t *testing.T, billingID string, daysLeft int) {
	t.Helper()
	record, err := f.hub.FindRecordById("system_billing", billingID)
	require.NoError(t, err)
	record.Set("expiresAt", time.Now().AddDate(0, 0, daysLeft))
	require.NoError(t, f.hub.Save(record))
}

func TestExpiryRemindersFollowTheLadder(t *testing.T) {
	f := newReminderFixture(t)
	system := f.system(t, "web-01", []string{f.user.Id})
	config := f.billingRecord(t, system.Id, 7)
	f.subscribe(t, system.Id, f.user.Id)

	// 7 days left: the first band fires once...
	messages := f.run(t)
	require.Len(t, messages, 1)
	require.Equal(t, f.user.Id, messages[0].UserID)
	require.Equal(t, system.Id, messages[0].SystemID)
	require.Equal(t, "System expires in 7 days: web-01", messages[0].Title)
	require.Contains(t, messages[0].Message, "web-01 (¥25/mo) expires on ")
	require.Contains(t, messages[0].Message, "(7 days left)")
	require.Equal(t, testLink("system", system.Id), messages[0].Link)
	require.Equal(t, 7, f.remindLevel(t, config.Id))

	// ...and running the job again the same day stays quiet.
	require.Empty(t, f.run(t))

	// A later day in the same band is still the same band.
	f.moveExpiry(t, config.Id, 6)
	require.Empty(t, f.run(t))

	// 3 and 1 days left are their own bands.
	f.moveExpiry(t, config.Id, 3)
	require.Len(t, f.run(t), 1)
	require.Equal(t, 3, f.remindLevel(t, config.Id))
	require.Empty(t, f.run(t))

	f.moveExpiry(t, config.Id, 1)
	messages = f.run(t)
	require.Len(t, messages, 1)
	require.Equal(t, "System expires in 1 day: web-01", messages[0].Title)
	require.Equal(t, 1, f.remindLevel(t, config.Id))

	// The expiry day itself is the last band.
	f.moveExpiry(t, config.Id, 0)
	messages = f.run(t)
	require.Len(t, messages, 1)
	require.Equal(t, "System expires today: web-01", messages[0].Title)
	require.Equal(t, billing.LevelExpired, f.remindLevel(t, config.Id))
	require.Empty(t, f.run(t))

	// Nothing is repeated once expired.
	f.moveExpiry(t, config.Id, -3)
	require.Empty(t, f.run(t))
}

func TestExpiryRemindersOnlyNotifySubscribers(t *testing.T) {
	f := newReminderFixture(t)
	system := f.system(t, "web-01", []string{f.user.Id})
	config := f.billingRecord(t, system.Id, 3)

	// The switch is off: nothing is sent and the band stays unspent, so a user
	// who opts in later still receives the reminder that is currently due.
	require.Empty(t, f.run(t))
	require.Equal(t, 0, f.remindLevel(t, config.Id))

	alert := f.subscribe(t, system.Id, f.user.Id)
	require.Len(t, f.run(t), 1)
	require.Equal(t, 3, f.remindLevel(t, config.Id))

	// Switching it back off (the API deletes the alert record) stops delivery.
	require.NoError(t, f.hub.Delete(alert))
	f.moveExpiry(t, config.Id, 1)
	require.Empty(t, f.run(t))
}

func TestExpiryRemindersNotifyEverySubscriber(t *testing.T) {
	f := newReminderFixture(t)
	second, err := tests.CreateUser(f.hub, "second@example.com", "password")
	require.NoError(t, err)

	system := f.system(t, "web-01", []string{f.user.Id, second.Id})
	f.billingRecord(t, system.Id, 7)
	f.subscribe(t, system.Id, f.user.Id)
	f.subscribe(t, system.Id, second.Id)

	messages := f.run(t)
	require.Len(t, messages, 2)
	users := []string{messages[0].UserID, messages[1].UserID}
	require.ElementsMatch(t, []string{f.user.Id, second.Id}, users)
	require.Equal(t, "web-01", messages[0].Title[len(messages[0].Title)-len("web-01"):])
}

func TestExpiryRemindersSkipSystemsWithoutExpiry(t *testing.T) {
	f := newReminderFixture(t)

	// Free systems and permanent cycles never expire, so no band is ever due.
	freeSystem := f.system(t, "free-01", []string{f.user.Id})
	_, err := tests.CreateRecord(f.hub, "system_billing", map[string]any{
		"system":      freeSystem.Id,
		"billingType": billing.BillingTypeFree,
		"expiresAt":   time.Now().AddDate(0, 0, 1),
	})
	require.NoError(t, err)
	f.subscribe(t, freeSystem.Id, f.user.Id)

	permanentSystem := f.system(t, "permanent-01", []string{f.user.Id})
	_, err = tests.CreateRecord(f.hub, "system_billing", map[string]any{
		"system":           permanentSystem.Id,
		"billingType":      billing.BillingTypePaid,
		"price":            25,
		"currency":         string(billing.CurrencyCNY),
		"cycle":            string(billing.CyclePermanent),
		"notifyDaysBefore": billing.DefaultNotifyDaysBefore,
	})
	require.NoError(t, err)
	f.subscribe(t, permanentSystem.Id, f.user.Id)

	require.Empty(t, f.run(t))
}

func TestExpiryRemindersSurviveSendFailures(t *testing.T) {
	f := newReminderFixture(t)
	system := f.system(t, "web-01", []string{f.user.Id})
	config := f.billingRecord(t, system.Id, 7)
	f.subscribe(t, system.Id, f.user.Id)

	// A failing send must not abort the scan: the ladder still advances so the
	// next day does not resend the same band forever.
	f.manager = billing.NewManager(f.hub, failingNotifier{}, testLink)
	require.Error(t, f.manager.CheckExpiryReminders())
	require.Equal(t, 7, f.remindLevel(t, config.Id))
}

type failingNotifier struct{}

func (failingNotifier) SendAlert(alerts.AlertMessageData) error {
	return assert.AnError
}
