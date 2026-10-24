package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Creates the per-system traffic counters. Written only by the hub (sampling
// diff of the agents' OS interface counters); the API exposes it read-only for
// members so the UI can show usage without a custom endpoint.
func init() {
	m.Register(func(app core.App) error {
		systems, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}

		collection := core.NewBaseCollection("system_traffic")
		collection.Fields.Add(
			// See the system_billing migration: base collections start with id only.
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
			&core.RelationField{
				Name:          "system",
				CollectionId:  systems.Id,
				MaxSelect:     1,
				CascadeDelete: true,
				Required:      true,
			},
			&core.DateField{Name: "periodStart"},
			&core.DateField{Name: "periodEnd"},
			&core.NumberField{Name: "usedUpload", OnlyInt: true},
			&core.NumberField{Name: "usedDownload", OnlyInt: true},
			// {"eth0":[sentTotal,recvTotal], ...} from the previous sample
			&core.JSONField{Name: "lastCounters", MaxSize: 200000},
			&core.DateField{Name: "lastSampleAt"},
			&core.DateField{Name: "lastResetAt"},
			&core.SelectField{Name: "resetReason", Values: []string{"auto", "manual", "init"}, MaxSelect: 1},
		)
		collection.Indexes = append(
			collection.Indexes,
			"CREATE UNIQUE INDEX `idx_traffic_system` ON `system_traffic` (`system`)",
		)
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("system_traffic")
		if err != nil {
			// already gone
			return nil
		}
		return app.Delete(collection)
	})
}
