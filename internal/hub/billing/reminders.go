package billing

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/henrygd/beszel/internal/alerts"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// BillingExpiryAlertName is the `alerts` name that opts one user into the
// reminders for one system. It matches alertInfo.BillingExpiry in the frontend
// and the value added to the alerts.name select by migration 1793010002.
const BillingExpiryAlertName = "BillingExpiry"

// paidWithExpiry matches the configs the reminder job cares about: a paid
// cycle that actually ends. Free and permanent systems never expire.
const paidWithExpiry = "billingType = 'paid' AND cycle != 'permanent' AND expiresAt != ''"

// Notifier delivers one message to a user's configured channels (email /
// webhook, silence rules included). Implemented by *alerts.AlertManager.
type Notifier interface {
	SendAlert(data alerts.AlertMessageData) error
}

// Manager runs the scheduled billing jobs. The hub creates it once.
type Manager struct {
	app      core.App
	notifier Notifier
	makeLink func(parts ...string) string
}

// NewManager wires the reminder job to the app, the alert delivery path and the
// hub link builder.
func NewManager(app core.App, notifier Notifier, makeLink func(parts ...string) string) *Manager {
	return &Manager{app: app, notifier: notifier, makeLink: makeLink}
}

// CheckExpiryReminders walks every paid config once a day and notifies the
// users who enabled the BillingExpiry alert for that system.
//
// Reminder bands are notifyDaysBefore / 3 / 1 / expired. `remindLevel` stores
// the band already sent, so a band fires once and a skipped day (hub was down)
// lands in the band that is currently due instead of replaying every band.
func (bm *Manager) CheckExpiryReminders() error {
	records, err := bm.app.FindAllRecords("system_billing", dbx.NewExp(paidWithExpiry))
	if err != nil {
		return err
	}
	now := time.Now()
	var errs []error
	for _, record := range records {
		if err := bm.checkExpiry(record, now); err != nil {
			errs = append(errs, fmt.Errorf("system %s: %w", record.GetString("system"), err))
		}
	}
	return errors.Join(errs...)
}

func (bm *Manager) checkExpiry(record *core.Record, now time.Time) error {
	cfg := configFromRecord(record)
	if !cfg.HasExpiry() {
		return nil
	}
	daysLeft := DaysUntilExpiry(cfg.ExpiresAt, now)
	level := ReminderLevel(daysLeft, cfg.NotifyDaysBefore)
	// LevelNone means no band is due. A stored level blocks itself and every
	// less urgent band (7 > 3 > 1 > -1), but LevelNone is also the unset value:
	// a config that was never reminded must still receive its first band.
	if level == LevelNone || (cfg.RemindLevel != LevelNone && level >= cfg.RemindLevel) {
		return nil
	}
	// Users can switch the reminder off for a system, which deletes their alert
	// record. Leave the band unspent so a later opt-in still receives it.
	recipients, err := bm.recipients(cfg.SystemId)
	if err != nil || len(recipients) == 0 {
		return err
	}
	system, err := bm.app.FindRecordById("systems", cfg.SystemId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Deleted systems cascade to their billing record; be defensive anyway.
			return nil
		}
		return err
	}
	title, message := expiryMessage(system.GetString("name"), cfg, daysLeft, now)

	// Persist before delivering: a failed delivery loses one band, while a
	// failed save would resend every day until it succeeds.
	record.Set("remindLevel", level)
	record.Set("remindAt", now.UTC())
	if err := bm.app.SaveNoValidate(record); err != nil {
		return err
	}
	var errs []error
	for _, userID := range recipients {
		err := bm.notifier.SendAlert(alerts.AlertMessageData{
			UserID:   userID,
			SystemID: cfg.SystemId,
			Title:    title,
			Message:  message,
			Link:     bm.makeLink("system", cfg.SystemId),
			LinkText: "View " + system.GetString("name"),
		})
		if err != nil {
			bm.app.Logger().Error("Failed to send billing expiry reminder", "system", cfg.SystemId, "user", userID, "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// recipients returns the users who enabled the billing expiry alert for a system.
func (bm *Manager) recipients(systemID string) ([]string, error) {
	records, err := bm.app.FindAllRecords("alerts", dbx.HashExp{"system": systemID, "name": BillingExpiryAlertName})
	if err != nil {
		return nil, err
	}
	users := make([]string, 0, len(records))
	for _, record := range records {
		if userID := record.GetString("user"); userID != "" {
			users = append(users, userID)
		}
	}
	return users, nil
}

var currencySymbols = map[Currency]string{
	CurrencyCNY: "¥",
	CurrencyUSD: "$",
	CurrencyEUR: "€",
	CurrencyGBP: "£",
}

var cycleSuffixes = map[Cycle]string{
	CycleMonthly:    "/mo",
	CycleSemiannual: "/6mo",
	CycleAnnual:     "/yr",
	CycleBiennial:   "/2yr",
	CycleTriennial:  "/3yr",
}

// costLabel renders the configured cost as "¥25/mo", or "" when no price is set.
func costLabel(cfg *Config) string {
	if cfg.Price <= 0 {
		return ""
	}
	symbol, ok := currencySymbols[cfg.Currency]
	if !ok {
		symbol = string(cfg.Currency) + " "
	}
	return symbol + strconv.FormatFloat(cfg.Price, 'f', -1, 64) + cycleSuffixes[cfg.Cycle]
}

// expiryMessage builds the notification text. Alert bodies are assembled here, in
// the hub, and handed to SendAlert as plain strings, so they never pass through
// the frontend i18n catalog. Every other alert type does the same (see
// alerts_system.go, alerts_network_monitors.go), and keeping this one in English
// is deliberate: translating it here alone would make billing reminders the only
// localized notification in the product.
func expiryMessage(systemName string, cfg *Config, daysLeft int, now time.Time) (title, message string) {
	expiry := time.UnixMilli(cfg.ExpiresAt).In(now.Location()).Format(time.DateOnly)
	subject := systemName
	if cost := costLabel(cfg); cost != "" {
		subject = fmt.Sprintf("%s (%s)", systemName, cost)
	}
	const hint = "Update the expiry date after renewing."
	switch {
	case daysLeft < 0:
		return fmt.Sprintf("System expired: %s", systemName),
			fmt.Sprintf("%s expired on %s (%s ago). %s", subject, expiry, dayCount(-daysLeft), hint)
	case daysLeft == 0:
		return fmt.Sprintf("System expires today: %s", systemName),
			fmt.Sprintf("%s expires today (%s). %s", subject, expiry, hint)
	default:
		return fmt.Sprintf("System expires in %s: %s", dayCount(daysLeft), systemName),
			fmt.Sprintf("%s expires on %s (%s left). %s", subject, expiry, dayCount(daysLeft), hint)
	}
}

func dayCount(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
