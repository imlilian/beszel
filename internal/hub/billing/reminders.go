package billing

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
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

// TrafficQuotaAlertName is the `alerts` name that opts one user into traffic
// usage reminders for one system (migration 1793010003 +
// alertInfo.TrafficQuota).
const TrafficQuotaAlertName = "TrafficQuota"

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
	recipients, err := bm.recipients(cfg.SystemId, BillingExpiryAlertName)
	if err != nil || len(recipients) == 0 {
		return err
	}
	system, err := bm.systemRecord(cfg.SystemId)
	if err != nil || system == nil {
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

// CheckTrafficReminders walks the systems with a quota and notifies the users
// who enabled the TrafficQuota alert for that system.
//
// Bands are 80% and "over the quota"; `trafficRemindLevel` stores the band
// already sent and is cleared by the sampler when the period rolls over.
func (bm *Manager) CheckTrafficReminders() error {
	configs, err := bm.app.FindAllRecords("system_billing", dbx.NewExp("trafficLimit > 0"))
	if err != nil {
		return err
	}
	if len(configs) == 0 {
		return nil
	}
	bySystem := make(map[string]*core.Record, len(configs))
	for _, record := range configs {
		bySystem[record.GetString("system")] = record
	}
	// `system_traffic` only exists for systems with a quota, so one scan of the
	// collection beats a lookup per config (and skips unlimited systems for free).
	trafficRecords, err := bm.app.FindAllRecords("system_traffic")
	if err != nil {
		return err
	}
	now := time.Now()
	var errs []error
	for _, trafficRecord := range trafficRecords {
		systemID := trafficRecord.GetString("system")
		cfgRecord, ok := bySystem[systemID]
		if !ok {
			continue
		}
		if err := bm.checkTraffic(cfgRecord, trafficRecord, now); err != nil {
			errs = append(errs, fmt.Errorf("system %s: %w", systemID, err))
		}
	}
	return errors.Join(errs...)
}

func (bm *Manager) checkTraffic(cfgRecord, trafficRecord *core.Record, now time.Time) error {
	cfg := configFromRecord(cfgRecord)
	if cfg.TrafficLimit <= 0 {
		return nil
	}
	traffic := trafficFromRecord(trafficRecord)
	used := trafficUsed(cfg, traffic)
	pct := float64(used) / float64(cfg.TrafficLimit) * 100
	level := trafficLevel(pct)
	// Same ladder rule as the expiry job: TrafficLevelNone (0) means "nothing
	// due" and is also the unset value, so it must not block the first band.
	if level == LevelNone || (cfg.TrafficRemindLevel != LevelNone && level >= cfg.TrafficRemindLevel) {
		return nil
	}
	recipients, err := bm.recipients(cfg.SystemId, TrafficQuotaAlertName)
	if err != nil || len(recipients) == 0 {
		return err
	}
	system, err := bm.systemRecord(cfg.SystemId)
	if err != nil || system == nil {
		return err
	}
	title, message := trafficMessage(system.GetString("name"), cfg, traffic, used, pct, now)

	// Persist before delivering (see checkExpiry).
	cfgRecord.Set("trafficRemindLevel", level)
	if err := bm.app.SaveNoValidate(cfgRecord); err != nil {
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
			bm.app.Logger().Error("Failed to send traffic reminder", "system", cfg.SystemId, "user", userID, "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// trafficLevel maps a usage percentage to the band it has reached.
func trafficLevel(pct float64) int {
	switch {
	case pct >= 100:
		return TrafficLevelExceeded
	case pct >= 80:
		return TrafficLevelWarn
	default:
		return LevelNone
	}
}

// trafficUsed returns the billable byte count for the configured mode.
func trafficUsed(cfg *Config, traffic *Traffic) int64 {
	if cfg.TrafficMode == TrafficModeUpload {
		return traffic.UsedUpload
	}
	return traffic.UsedUpload + traffic.UsedDownload
}

// recipients returns the users who enabled `alertName` for a system.
func (bm *Manager) recipients(systemID, alertName string) ([]string, error) {
	records, err := bm.app.FindAllRecords("alerts", dbx.HashExp{"system": systemID, "name": alertName})
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

// systemRecord loads the system a config belongs to, returning nil when it is
// gone (deleting a system cascades to its billing record, so this is only a
// safety net) instead of failing the whole scan.
func (bm *Manager) systemRecord(systemID string) (*core.Record, error) {
	record, err := bm.app.FindRecordById("systems", systemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
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

// trafficMessage builds the notification text. Same convention as
// expiryMessage: plain English assembled here, because the delivery path has no
// frontend and the i18n catalog never reaches it.
//
// Shape: "web-01 has used 812 GB of 1 TB (81%) this period, resets in 18 days."
func trafficMessage(
	systemName string,
	cfg *Config,
	traffic *Traffic,
	used int64,
	pct float64,
	now time.Time,
) (title, message string) {
	percent := strconv.Itoa(int(math.Round(pct)))
	usage := fmt.Sprintf("%s of %s (%s%%)", formatBytes(used), formatBytes(cfg.TrafficLimit), percent)
	reset := ""
	if traffic.PeriodEnd > 0 {
		if days := DaysUntilExpiry(traffic.PeriodEnd, now); days > 0 {
			reset = ", resets in " + dayCount(days)
		} else {
			reset = ", resets today"
		}
	}
	if pct >= 100 {
		return fmt.Sprintf("Traffic quota exceeded: %s", systemName),
			fmt.Sprintf("%s has used %s this period%s. Further traffic may be billed by your provider.", systemName, usage, reset)
	}
	return fmt.Sprintf("Traffic at %s%%: %s", percent, systemName),
		fmt.Sprintf("%s has used %s this period%s.", systemName, usage, reset)
}

// formatBytes renders a byte count the way the dashboard does: 1024-based, at
// most one decimal, and no trailing ".0".
func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	// One division per unit step: 1024 bytes is 1 KB, 1024^2 is 1 MB, ...
	units := [...]string{"KB", "MB", "GB", "TB", "PB", "EB"}
	value := float64(bytes)
	steps := 0
	for value >= 1024 && steps < len(units) {
		value /= 1024
		steps++
	}
	// Whole numbers stay whole ("1 TB"), everything else keeps one decimal.
	rounded := math.Round(value*10) / 10
	return strconv.FormatFloat(rounded, 'f', -1, 64) + " " + units[steps-1]
}
