//go:build testing

package billing_test

import (
	"testing"
	"time"

	"github.com/henrygd/beszel/internal/alerts"
	"github.com/henrygd/beszel/internal/hub/billing"
	"github.com/henrygd/beszel/internal/tests"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

// 1024 GB is exactly 1 TiB, so the percentages below stay readable.
const tebibyte = int64(1) << 40

// quotaConfig creates a paid-but-permanent config with a traffic quota, which
// is what the traffic reminder job scans for. Permanent keeps the expiry job
// out of the picture.
func (f *reminderFixture) quotaConfig(t *testing.T, systemID string, limit int64, mode billing.TrafficMode) *core.Record {
	t.Helper()
	record, err := tests.CreateRecord(f.hub, "system_billing", map[string]any{
		"system":       systemID,
		"billingType":  billing.BillingTypePaid,
		"price":        25,
		"currency":     string(billing.CurrencyCNY),
		"cycle":        string(billing.CyclePermanent),
		"trafficLimit": limit,
		"trafficMode":  string(mode),
		"resetDay":     14,
	})
	require.NoError(t, err)
	return record
}

// ledger writes the `system_traffic` record the sampler would have produced.
func (f *reminderFixture) ledger(
	t *testing.T,
	systemID string,
	up, down int64,
	periodEnd time.Time,
) *core.Record {
	t.Helper()
	start := periodEnd.AddDate(0, -1, 0)
	record, err := tests.CreateRecord(f.hub, "system_traffic", map[string]any{
		"system":       systemID,
		"periodStart":  start,
		"periodEnd":    periodEnd,
		"usedUpload":   up,
		"usedDownload": down,
		"lastResetAt":  start,
		"resetReason":  billing.ResetReasonInit,
	})
	require.NoError(t, err)
	return record
}

// subscribeTraffic turns the TrafficQuota switch on for one user and system.
func (f *reminderFixture) subscribeTraffic(t *testing.T, systemID, userID string) *core.Record {
	t.Helper()
	alert, err := tests.CreateRecord(f.hub, "alerts", map[string]any{
		"system": systemID,
		"user":   userID,
		"name":   billing.TrafficQuotaAlertName,
		"value":  0,
		"min":    0,
	})
	require.NoError(t, err)
	return alert
}

func (f *reminderFixture) runTraffic(t *testing.T) []alerts.AlertMessageData {
	t.Helper()
	require.NoError(t, f.manager.CheckTrafficReminders())
	return f.notifier.take()
}

// trafficLevelOf reads the persisted band back from the config record.
func (f *reminderFixture) trafficLevelOf(t *testing.T, billingID string) int {
	t.Helper()
	record, err := f.hub.FindRecordById("system_billing", billingID)
	require.NoError(t, err)
	return record.GetInt("trafficRemindLevel")
}

// setUsage rewrites the ledger, i.e. "the agent kept sending samples".
func (f *reminderFixture) setUsage(t *testing.T, trafficID string, up, down int64) {
	t.Helper()
	record, err := f.hub.FindRecordById("system_traffic", trafficID)
	require.NoError(t, err)
	record.Set("usedUpload", up)
	record.Set("usedDownload", down)
	require.NoError(t, f.hub.Save(record))
}

func TestTrafficRemindersFollowTheLadder(t *testing.T) {
	f := newReminderFixture(t)
	system := f.system(t, "web-01", []string{f.user.Id})
	config := f.quotaConfig(t, system.Id, tebibyte, billing.TrafficModeSum)
	ledger := f.ledger(t, system.Id, 0, 0, time.Now().AddDate(0, 0, 18))
	f.subscribeTraffic(t, system.Id, f.user.Id)

	// Below 80% nothing is due.
	f.setUsage(t, ledger.Id, 808*(1<<30), 0)
	require.Empty(t, f.runTraffic(t))
	require.Equal(t, 0, f.trafficLevelOf(t, config.Id))

	// 80% fires once, with the usage split rendered for humans.
	f.setUsage(t, ledger.Id, 820*(1<<30), 0)
	messages := f.runTraffic(t)
	require.Len(t, messages, 1)
	require.Equal(t, f.user.Id, messages[0].UserID)
	require.Equal(t, system.Id, messages[0].SystemID)
	require.Equal(t, "Traffic at 80%: web-01", messages[0].Title)
	require.Contains(t, messages[0].Message, "web-01 has used 820 GB of 1 TB (80%) this period, resets in 18 days.")
	require.Equal(t, testLink("system", system.Id), messages[0].Link)
	require.Equal(t, billing.TrafficLevelWarn, f.trafficLevelOf(t, config.Id))

	// Running the job again in the same band stays quiet, even as usage grows.
	require.Empty(t, f.runTraffic(t))
	f.setUsage(t, ledger.Id, 900*(1<<30), 0)
	require.Empty(t, f.runTraffic(t))

	// Crossing the quota is its own (more urgent) band.
	f.setUsage(t, ledger.Id, 1200*(1<<30), 0)
	messages = f.runTraffic(t)
	require.Len(t, messages, 1)
	require.Equal(t, "Traffic quota exceeded: web-01", messages[0].Title)
	require.Contains(t, messages[0].Message, "web-01 has used 1.2 TB of 1 TB (117%) this period, resets in 18 days.")
	require.Equal(t, billing.TrafficLevelExceeded, f.trafficLevelOf(t, config.Id))

	// Nothing is repeated once the quota is blown.
	require.Empty(t, f.runTraffic(t))
	f.setUsage(t, ledger.Id, 2000*(1<<30), 0)
	require.Empty(t, f.runTraffic(t))
}

func TestTrafficRemindersOnlyNotifySubscribers(t *testing.T) {
	f := newReminderFixture(t)
	system := f.system(t, "web-01", []string{f.user.Id})
	config := f.quotaConfig(t, system.Id, tebibyte, billing.TrafficModeSum)
	ledger := f.ledger(t, system.Id, 900*(1<<30), 0, time.Now().AddDate(0, 0, 18))

	// The switch is off: nothing is sent and the band stays unspent, so a user
	// who opts in later still gets the reminder that is currently due.
	require.Empty(t, f.runTraffic(t))
	require.Equal(t, 0, f.trafficLevelOf(t, config.Id))

	alert := f.subscribeTraffic(t, system.Id, f.user.Id)
	require.Len(t, f.runTraffic(t), 1)
	require.Equal(t, billing.TrafficLevelWarn, f.trafficLevelOf(t, config.Id))

	// Switching it off (the API deletes the alert record) stops delivery.
	require.NoError(t, f.hub.Delete(alert))
	f.setUsage(t, ledger.Id, 1100*(1<<30), 0)
	require.Empty(t, f.runTraffic(t))
}

func TestTrafficRemindersRespectTrafficMode(t *testing.T) {
	f := newReminderFixture(t)
	system := f.system(t, "web-01", []string{f.user.Id})
	// 900 GB up + 900 GB down is 88% of the quota when only upload counts.
	f.quotaConfig(t, system.Id, tebibyte, billing.TrafficModeUpload)
	f.ledger(t, system.Id, 900*(1<<30), 900*(1<<30), time.Now().AddDate(0, 0, 5))
	f.subscribeTraffic(t, system.Id, f.user.Id)

	messages := f.runTraffic(t)
	require.Len(t, messages, 1)
	require.Contains(t, messages[0].Message, "web-01 has used 900 GB of 1 TB (88%)")
}

func TestTrafficRemindersSkipUnlimitedSystems(t *testing.T) {
	f := newReminderFixture(t)
	system := f.system(t, "web-01", []string{f.user.Id})
	// Quota turned back off: the ledger survives but must not notify.
	f.quotaConfig(t, system.Id, 0, billing.TrafficModeSum)
	f.ledger(t, system.Id, 900*(1<<30), 0, time.Now().AddDate(0, 0, 18))
	f.subscribeTraffic(t, system.Id, f.user.Id)

	require.Empty(t, f.runTraffic(t))
}

// TestTrafficRemindersResetWithThePeriod pins the contract between the sampler
// and the reminder job: a new period clears the ladder, otherwise the previous
// period's 80% band would silence this one.
func TestTrafficRemindersResetWithThePeriod(t *testing.T) {
	f := newTrafficFixture(t, tebibyte)
	config, err := f.hub.FindFirstRecordByFilter(
		"system_billing",
		"system = {:system}",
		dbx.Params{"system": f.system.Id},
	)
	require.NoError(t, err)
	config.Set("trafficRemindLevel", billing.TrafficLevelWarn)
	require.NoError(t, f.hub.Save(config))

	f.sample(t, map[string][2]uint64{"eth0": {1000, 2000}})
	f.now = f.now.AddDate(0, 2, 0)
	f.sample(t, map[string][2]uint64{"eth0": {9000, 9000}})

	reloaded, err := f.hub.FindRecordById("system_billing", config.Id)
	require.NoError(t, err)
	require.Equal(t, billing.LevelNone, reloaded.GetInt("trafficRemindLevel"))
	require.Equal(t, billing.ResetReasonAuto, f.traffic(t).ResetReason)
}
