package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Creates the per-system billing config collection. It holds both the cost
// fields and the traffic quota fields, since they are edited in the same dialog
// and fetched together. 1:1 with systems (unique index), cascade deleted.
func init() {
	m.Register(func(app core.App) error {
		systems, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}

		zero, one, thirtyOne, sixty := 0.0, 1.0, 31.0, 60.0

		collection := core.NewBaseCollection("system_billing")
		collection.Fields.Add(
			// NewBaseCollection only adds the id field, so the timestamps the
			// dashboard collections normally carry have to be declared here.
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
			&core.RelationField{
				Name:          "system",
				CollectionId:  systems.Id,
				MaxSelect:     1,
				CascadeDelete: true,
				Required:      true,
			},
			// cost
			&core.SelectField{Name: "billingType", Values: []string{"free", "paid"}, MaxSelect: 1},
			&core.NumberField{Name: "price", Min: &zero},
			&core.SelectField{Name: "currency", Values: []string{"CNY", "USD", "EUR", "GBP"}, MaxSelect: 1},
			&core.SelectField{
				Name:      "cycle",
				Values:    []string{"monthly", "semiannual", "annual", "biennial", "triennial", "permanent"},
				MaxSelect: 1,
			},
			&core.DateField{Name: "paidAt"},
			&core.DateField{Name: "expiresAt"},
			&core.NumberField{Name: "notifyDaysBefore", OnlyInt: true, Min: &zero, Max: &sixty},
			&core.NumberField{Name: "remindLevel", OnlyInt: true},
			&core.DateField{Name: "remindAt"},
			// traffic quota
			&core.NumberField{Name: "trafficLimit", OnlyInt: true, Min: &zero},
			&core.SelectField{Name: "trafficMode", Values: []string{"sum", "upload"}, MaxSelect: 1},
			&core.NumberField{Name: "resetDay", OnlyInt: true, Min: &one, Max: &thirtyOne},
			&core.BoolField{Name: "trafficAlerts"},
			&core.NumberField{Name: "trafficRemindLevel", OnlyInt: true},
			&core.DateField{Name: "manualResetAt"},
		)
		collection.Indexes = append(
			collection.Indexes,
			"CREATE UNIQUE INDEX `idx_billing_system` ON `system_billing` (`system`)",
		)
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("system_billing")
		if err != nil {
			// already gone
			return nil
		}
		return app.Delete(collection)
	})
}
