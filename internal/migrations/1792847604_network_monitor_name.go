package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds an optional display name to network monitors (e.g. "Shanghai Telecom").
// The name is hub-only metadata and is not part of the monitor ID, so renaming
// a monitor never recreates it or breaks its collected stats.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("network_monitors")
		if err != nil {
			return err
		}
		collection.Fields.Add(&core.TextField{Id: "nm_name", Name: "name", Max: 100})
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("network_monitors")
		if err != nil {
			return err
		}
		collection.Fields.RemoveById("nm_name")
		return app.Save(collection)
	})
}
