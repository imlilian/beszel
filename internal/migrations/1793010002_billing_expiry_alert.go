package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds the BillingExpiry alert type so billing reminders reuse the existing
// per-user, per-system alert switches instead of a separate opt-in field.
// Mirrors 1791926360_network_monitor_cert_alert.go.
const billingExpiryAlertName = "BillingExpiry"

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("name").(*core.SelectField)
		if !slices.Contains(field.Values, billingExpiryAlertName) {
			field.Values = append(field.Values, billingExpiryAlertName)
		}
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		field := collection.Fields.GetByName("name").(*core.SelectField)
		field.Values = slices.DeleteFunc(field.Values, func(name string) bool {
			return name == billingExpiryAlertName
		})
		return app.Save(collection)
	})
}
