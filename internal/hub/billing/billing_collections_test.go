//go:build testing

package billing_test

import (
	"testing"

	"github.com/henrygd/beszel/internal/hub/billing"
	"github.com/henrygd/beszel/internal/tests"
	"github.com/stretchr/testify/require"
)

// The collections are created by migrations, and the rules are applied when the
// hub starts, so this covers both halves of "the data can be stored and read".
func TestBillingCollections(t *testing.T) {
	hub, user := tests.GetHubWithUser(t)
	defer hub.Cleanup()

	billingCollection, err := hub.FindCollectionByNameOrId("system_billing")
	require.NoError(t, err)
	trafficCollection, err := hub.FindCollectionByNameOrId("system_traffic")
	require.NoError(t, err)

	// system_billing: members can read and manage their systems' config.
	require.NotNil(t, billingCollection.ListRule)
	require.NotNil(t, billingCollection.CreateRule)
	require.NotNil(t, billingCollection.UpdateRule)
	require.NotNil(t, billingCollection.DeleteRule)

	// system_traffic: hub-only writes, member reads.
	require.NotNil(t, trafficCollection.ListRule)
	require.Nil(t, trafficCollection.CreateRule)
	require.Nil(t, trafficCollection.UpdateRule)
	require.Nil(t, trafficCollection.DeleteRule)

	require.NotNil(t, billingCollection.Fields.GetByName("trafficLimit"))
	require.NotNil(t, trafficCollection.Fields.GetByName("lastCounters"))
	// Base collections only get `id` by default; the timestamps must be declared.
	require.NotNil(t, billingCollection.Fields.GetByName("created"))
	require.NotNil(t, billingCollection.Fields.GetByName("updated"))
	require.NotNil(t, trafficCollection.Fields.GetByName("created"))
	require.NotNil(t, trafficCollection.Fields.GetByName("updated"))

	systems, err := tests.CreateSystems(hub, 1, user.Id, "up")
	require.NoError(t, err)
	systemId := systems[0].Id

	_, err = tests.CreateRecord(hub, "system_billing", map[string]any{
		"system":        systemId,
		"billingType":   "paid",
		"price":         25,
		"currency":      "CNY",
		"cycle":         "monthly",
		"trafficLimit":  1_099_511_627_776, // 1 TiB
		"trafficMode":   "sum",
		"resetDay":      14,
		"trafficAlerts": true,
	})
	require.NoError(t, err)

	// The unique index keeps billing 1:1 with systems.
	_, err = tests.CreateRecord(hub, "system_billing", map[string]any{"system": systemId})
	require.Error(t, err)

	// Traffic is 1:1 as well.
	_, err = tests.CreateRecord(hub, "system_traffic", map[string]any{"system": systemId, "usedUpload": 10})
	require.NoError(t, err)
	_, err = tests.CreateRecord(hub, "system_traffic", map[string]any{"system": systemId})
	require.Error(t, err)
}

// A missing billing record is the normal case and must not be an error.
func TestLoadMissingBillingReturnsNil(t *testing.T) {
	hub, _ := tests.GetHubWithUser(t)
	defer hub.Cleanup()

	config, err := billing.Load(hub, "nonexistent")
	require.NoError(t, err)
	require.Nil(t, config)

	traffic, err := billing.LoadTraffic(hub, "nonexistent")
	require.NoError(t, err)
	require.Nil(t, traffic)
}
