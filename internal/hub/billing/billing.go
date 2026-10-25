// Package billing holds the typed view of the per-system billing and traffic
// metadata (collections `system_billing` and `system_traffic`), plus the pure
// calendar helpers used by the expiry reminders and the period rollover.
//
// The scheduled jobs live in this package too (see reminders.go). They never
// talk to the network directly: notifications go through the alerts manager,
// which applies the user's channels, silence rules and quiet hours.
package billing

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type Currency string

const (
	CurrencyCNY Currency = "CNY"
	CurrencyUSD Currency = "USD"
	CurrencyEUR Currency = "EUR"
	CurrencyGBP Currency = "GBP"
)

type Cycle string

const (
	CycleMonthly    Cycle = "monthly"
	CycleSemiannual Cycle = "semiannual"
	CycleAnnual     Cycle = "annual"
	CycleBiennial   Cycle = "biennial"
	CycleTriennial  Cycle = "triennial"
	/** No expiry date at all. */
	CyclePermanent Cycle = "permanent"
)

type TrafficMode string

const (
	/** Count upload + download against the quota. */
	TrafficModeSum TrafficMode = "sum"
	/** Count upload only; download is still recorded but not billed. */
	TrafficModeUpload TrafficMode = "upload"
)

const (
	BillingTypeFree = "free"
	BillingTypePaid = "paid"
)

// Reminder levels. Smaller is more urgent, so a level already emitted blocks
// every level >= it (see ReminderLevel).
//
// LevelNone is also the stored default of `remindLevel`, i.e. "no band sent
// yet", which is a different thing from "nothing was due".
const (
	LevelExpired = -1 // already expired
	LevelNone    = 0  // no reminder needed
)

const (
	DefaultNotifyDaysBefore = 7
	DefaultResetDay         = 1
)

// Config is the typed view of a `system_billing` record. Timestamps are Unix
// milliseconds, and 0 means "not set".
type Config struct {
	SystemId           string      `db:"system"`
	BillingType        string      `db:"billingType"`
	Price              float64     `db:"price"`
	Currency           Currency    `db:"currency"`
	Cycle              Cycle       `db:"cycle"`
	PaidAt             int64       `db:"paidAt"`
	ExpiresAt          int64       `db:"expiresAt"`
	NotifyDaysBefore   int         `db:"notifyDaysBefore"`
	RemindLevel        int         `db:"remindLevel"`
	RemindAt           int64       `db:"remindAt"`
	TrafficLimit       int64       `db:"trafficLimit"`
	TrafficMode        TrafficMode `db:"trafficMode"`
	ResetDay           int         `db:"resetDay"`
	TrafficAlerts      bool        `db:"trafficAlerts"`
	TrafficRemindLevel int         `db:"trafficRemindLevel"`
	ManualResetAt      int64       `db:"manualResetAt"`
}

// HasExpiry reports whether the config has an expiry date to track.
func (c *Config) HasExpiry() bool {
	return c.ExpiresAt > 0 && c.Cycle != CyclePermanent && c.BillingType != BillingTypeFree
}

// Traffic is the typed view of a `system_traffic` record.
type Traffic struct {
	SystemId     string               `db:"system"`
	PeriodStart  int64                `db:"periodStart"`
	PeriodEnd    int64                `db:"periodEnd"`
	UsedUpload   int64                `db:"usedUpload"`
	UsedDownload int64                `db:"usedDownload"`
	LastCounters map[string][2]uint64 `db:"lastCounters"`
	LastSampleAt int64                `db:"lastSampleAt"`
	LastResetAt  int64                `db:"lastResetAt"`
	ResetReason  string               `db:"resetReason"`
}

// Load returns the billing config for a system, or nil when the system has none
// (which is the normal state: billing is optional).
func Load(app core.App, systemID string) (*Config, error) {
	record, err := app.FindFirstRecordByFilter(
		"system_billing",
		"system = {:system}",
		dbx.Params{"system": systemID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return configFromRecord(record), nil
}

// LoadTraffic returns the traffic counters for a system, or nil when absent.
func LoadTraffic(app core.App, systemID string) (*Traffic, error) {
	record, err := app.FindFirstRecordByFilter(
		"system_traffic",
		"system = {:system}",
		dbx.Params{"system": systemID},
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &Traffic{
		SystemId:     record.GetString("system"),
		PeriodStart:  dateMillis(record, "periodStart"),
		PeriodEnd:    dateMillis(record, "periodEnd"),
		UsedUpload:   int64(record.GetInt("usedUpload")),
		UsedDownload: int64(record.GetInt("usedDownload")),
		LastCounters: ParseCounters(record.Get("lastCounters")),
		LastSampleAt: dateMillis(record, "lastSampleAt"),
		LastResetAt:  dateMillis(record, "lastResetAt"),
		ResetReason:  record.GetString("resetReason"),
	}, nil
}

func configFromRecord(record *core.Record) *Config {
	return &Config{
		SystemId:           record.GetString("system"),
		BillingType:        record.GetString("billingType"),
		Price:              record.GetFloat("price"),
		Currency:           Currency(record.GetString("currency")),
		Cycle:              Cycle(record.GetString("cycle")),
		PaidAt:             dateMillis(record, "paidAt"),
		ExpiresAt:          dateMillis(record, "expiresAt"),
		NotifyDaysBefore:   record.GetInt("notifyDaysBefore"),
		RemindLevel:        record.GetInt("remindLevel"),
		RemindAt:           dateMillis(record, "remindAt"),
		TrafficLimit:       int64(record.GetFloat("trafficLimit")),
		TrafficMode:        TrafficMode(record.GetString("trafficMode")),
		ResetDay:           record.GetInt("resetDay"),
		TrafficAlerts:      record.GetBool("trafficAlerts"),
		TrafficRemindLevel: record.GetInt("trafficRemindLevel"),
		ManualResetAt:      dateMillis(record, "manualResetAt"),
	}
}

/** Zero PocketBase dates (unset) come back as 0 instead of year 1. */
func dateMillis(record *core.Record, name string) int64 {
	value := record.GetDateTime(name)
	if value.IsZero() {
		return 0
	}
	return value.Time().UnixMilli()
}

// ParseCounters converts the stored `lastCounters` JSON
// ({"eth0":[sentTotal,recvTotal]}) into typed counters. Unknown or malformed
// entries are skipped instead of failing the whole sample.
func ParseCounters(raw any) map[string][2]uint64 {
	counters := make(map[string][2]uint64)
	entries, ok := raw.(map[string]any)
	if !ok {
		return counters
	}
	for name, value := range entries {
		pair, ok := value.([]any)
		if !ok || len(pair) < 2 {
			continue
		}
		counters[name] = [2]uint64{toUint64(pair[0]), toUint64(pair[1])}
	}
	return counters
}

func toUint64(value any) uint64 {
	switch n := value.(type) {
	case float64:
		if n < 0 {
			return 0
		}
		return uint64(n)
	case int64:
		if n < 0 {
			return 0
		}
		return uint64(n)
	case json.Number:
		parsed, err := n.Int64()
		if err != nil || parsed < 0 {
			return 0
		}
		return uint64(parsed)
	default:
		return 0
	}
}

// DaysUntilExpiry returns whole days from "today 00:00" to "expiry 00:00" in
// now's location. Negative means already expired. Both dates are reduced to
// their calendar day and differenced in UTC, so a DST change can never shave a
// day off (23:59 today vs 00:01 tomorrow is exactly 1 day).
//
// Callers must check expiresAt > 0 first; a zero expiry returns 0.
func DaysUntilExpiry(expiresAtMs int64, now time.Time) int {
	if expiresAtMs <= 0 {
		return 0
	}
	loc := now.Location()
	expiry := time.UnixMilli(expiresAtMs).In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	expiryDay := time.Date(expiry.Year(), expiry.Month(), expiry.Day(), 0, 0, 0, 0, time.UTC)
	return int(expiryDay.Sub(today) / (24 * time.Hour))
}

// Period returns the current billing period [start, end) for a monthly reset
// on resetDay, both at 00:00 in loc.
//
// A resetDay past the end of a month is clamped to that month's last day
// (31 -> 28/29 in February, 30 in April), and the following period starts from
// the clamped day as well. There is no "roll over to the 1st": that would make
// consecutive periods overlap.
func Period(now time.Time, resetDay int, loc *time.Location) (time.Time, time.Time) {
	if loc == nil {
		loc = now.Location()
	}
	if resetDay < 1 {
		resetDay = 1
	}
	local := now.In(loc)
	start := resetPoint(local.Year(), local.Month(), resetDay, loc)
	if local.Before(start) {
		year, month := previousMonth(local.Year(), local.Month())
		start = resetPoint(year, month, resetDay, loc)
	}
	return start, resetPoint(start.Year(), start.Month()+1, resetDay, loc)
}

// ReminderLevel maps days left to the reminder band the system is currently in:
// -1 expired, 1 (last day), 3, or notifyDaysBefore. 0 means no reminder.
//
// Bands are checked from the most urgent one, so a system that skipped a day
// (hub was down) lands in the band that is currently due instead of replaying
// every skipped band. Each band only exists when notifyDaysBefore covers it.
func ReminderLevel(daysLeft, notifyDaysBefore int) int {
	switch {
	case daysLeft <= 0:
		return LevelExpired
	case daysLeft <= 1 && notifyDaysBefore >= 1:
		return 1
	case daysLeft <= 3 && notifyDaysBefore >= 3:
		return 3
	case notifyDaysBefore > 0 && daysLeft <= notifyDaysBefore:
		return notifyDaysBefore
	default:
		return LevelNone
	}
}

// AddCycle returns the date one billing cycle after t, keeping the time of day.
// Day-of-month is clamped to the target month's last day (Jan 31 + 1 month =
// Feb 28/29), and CyclePermanent returns the zero time because there is no
// expiry to compute.
func AddCycle(t time.Time, c Cycle) time.Time {
	months := 0
	switch c {
	case CycleMonthly:
		months = 1
	case CycleSemiannual:
		months = 6
	case CycleAnnual:
		months = 12
	case CycleBiennial:
		months = 24
	case CycleTriennial:
		months = 36
	case CyclePermanent:
		return time.Time{}
	default:
		return time.Time{}
	}
	year, month := normalizeMonth(t.Year(), t.Month()+time.Month(months))
	day := t.Day()
	if last := daysInMonth(year, month); day > last {
		day = last
	}
	return time.Date(year, month, day, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

/** The reset instant of the given month, clamped to the month's last day. */
func resetPoint(year int, month time.Month, resetDay int, loc *time.Location) time.Time {
	year, month = normalizeMonth(year, month)
	day := resetDay
	if last := daysInMonth(year, month); day > last {
		day = last
	}
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

/** Accepts out-of-range months (0, 13, ...) the same way time.Date does. */
func normalizeMonth(year int, month time.Month) (int, time.Month) {
	t := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	return t.Year(), t.Month()
}

func previousMonth(year int, month time.Month) (int, time.Month) {
	return normalizeMonth(year, month-1)
}

/** Last calendar day of the month, independent of the local time zone. */
func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
