//go:build testing

package systems

import (
	"testing"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/henrygd/beszel/internal/hub/billing"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

// TestCreateRecordsAccumulatesTraffic checks the wiring: every stored sample
// also feeds the billing traffic ledger, inside the same transaction.
func TestCreateRecordsAccumulatesTraffic(t *testing.T) {
	sys, app := newTestSystemWithHub(t)
	// The notifier and link builder are never used by accumulation.
	sys.manager.SetTrafficAccumulator(billing.NewManager(app, nil, nil))

	collection, err := app.FindCachedCollectionByNameOrId("system_billing")
	require.NoError(t, err)
	config := core.NewRecord(collection)
	config.Load(map[string]any{
		"system":       sys.Id,
		"billingType":  billing.BillingTypePaid,
		"cycle":        string(billing.CycleMonthly),
		"expiresAt":    "2027-01-01 00:00:00.000Z",
		"trafficLimit": 1 << 30,
		"trafficMode":  string(billing.TrafficModeSum),
		"resetDay":     14,
	})
	require.NoError(t, app.SaveNoValidate(config))

	sample := func(up, down uint64) *system.CombinedData {
		return &system.CombinedData{Stats: system.Stats{
			NetworkInterfaces: map[string][4]uint64{"eth0": {0, 0, up, down}},
		}}
	}

	// The first sample is the baseline, the second one is billed.
	_, err = sys.createRecords(sample(1000, 2000))
	require.NoError(t, err)
	traffic, err := billing.LoadTraffic(app, sys.Id)
	require.NoError(t, err)
	require.NotNil(t, traffic, "the sample should create the traffic record")
	require.Equal(t, int64(0), traffic.UsedUpload)

	_, err = sys.createRecords(sample(1600, 2400))
	require.NoError(t, err)
	traffic, err = billing.LoadTraffic(app, sys.Id)
	require.NoError(t, err)
	require.Equal(t, int64(600), traffic.UsedUpload)
	require.Equal(t, int64(400), traffic.UsedDownload)

	// The monitoring data itself is unaffected by the extra write.
	stats, err := app.FindAllRecords("system_stats")
	require.NoError(t, err)
	require.Len(t, stats, 2)
}
