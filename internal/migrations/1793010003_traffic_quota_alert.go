package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds the TrafficQuota alert type so traffic usage reminders reuse the same
// per-user, per-system switches as every other alert, instead of the
// `trafficAlerts` boolean on system_billing (kept for compatibility, no longer
// read by the reminder job).
// Mirrors 1793010002_billing_expiry_alert.go.
const trafficQuotaAlertName = "TrafficQuota"

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("name").(*core.SelectField)
		if !slices.Contains(field.Values, trafficQuotaAlertName) {
			field.Values = append(field.Values, trafficQuotaAlertName)
		}
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("name").(*core.SelectField)
		field.Values = slices.DeleteFunc(field.Values, func(name string) bool {
			return name == trafficQuotaAlertName
		})
		return app.Save(collection)
	})
}
