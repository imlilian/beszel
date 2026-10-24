package billing

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// BindHooks enforces the invariants that must hold no matter which client
// writes the record -- the dialog is not the only possible writer, and the
// reminder cron trusts these fields.
func BindHooks(app core.App) {
	app.OnRecordCreateRequest("system_billing").BindFunc(func(e *core.RecordRequestEvent) error {
		applyDefaults(e.Record)
		if err := normalize(e.Record); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordUpdateRequest("system_billing").BindFunc(func(e *core.RecordRequestEvent) error {
		// Renewing (moving expiresAt out) has to allow reminders again: the level
		// records how far the ladder was already walked, and a new expiry starts a
		// new ladder. Compared on the date value, not the raw string, so a
		// timezone-only rewrite doesn't reset it.
		if !e.Record.GetDateTime("expiresAt").Equal(e.Record.Original().GetDateTime("expiresAt")) {
			e.Record.Set("remindLevel", LevelNone)
		}
		if err := normalize(e.Record); err != nil {
			return err
		}
		return e.Next()
	})
}

// applyDefaults fills the fields the UI would normally send, so a record created
// straight through the API is still consistent.
//
// `trafficAlerts` is intentionally not defaulted here: PocketBase reports an
// absent bool as false, which is indistinguishable from an explicit false.
func applyDefaults(record *core.Record) {
	if record.GetString("billingType") == "" {
		record.Set("billingType", BillingTypePaid)
	}
	if record.GetString("currency") == "" {
		record.Set("currency", string(CurrencyCNY))
	}
	if record.GetString("cycle") == "" {
		record.Set("cycle", string(CycleMonthly))
	}
	if record.GetString("trafficMode") == "" {
		record.Set("trafficMode", string(TrafficModeSum))
	}
	if record.GetInt("notifyDaysBefore") <= 0 {
		record.Set("notifyDaysBefore", DefaultNotifyDaysBefore)
	}
	if record.GetInt("resetDay") <= 0 {
		record.Set("resetDay", DefaultResetDay)
	}
}

// normalize clears fields that make no sense for the chosen billing type and
// rejects the combinations the reminder cron cannot handle.
func normalize(record *core.Record) error {
	cycle := Cycle(record.GetString("cycle"))
	expiresAt := record.GetDateTime("expiresAt")
	paidAt := record.GetDateTime("paidAt")

	switch record.GetString("billingType") {
	case BillingTypeFree:
		// Free systems show a single label, so keep the cost fields empty.
		record.Set("price", 0)
		record.Set("paidAt", types.DateTime{})
		record.Set("expiresAt", types.DateTime{})
		return nil
	case BillingTypePaid:
		if cycle == CyclePermanent {
			record.Set("expiresAt", types.DateTime{})
			return nil
		}
		if expiresAt.IsZero() {
			return apis.NewBadRequestError("expiresAt is required for a paid system that is not permanent", nil)
		}
		if !paidAt.IsZero() && !expiresAt.After(paidAt) {
			return apis.NewBadRequestError("expiresAt must be after paidAt", nil)
		}
		return nil
	default:
		return apis.NewBadRequestError("billingType must be either free or paid", nil)
	}
}
