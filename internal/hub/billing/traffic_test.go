//go:build testing

package billing_test

import (
	"testing"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/henrygd/beszel/internal/hub/billing"
	"github.com/henrygd/beszel/internal/tests"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

const gigabyte uint64 = 1 << 30

type trafficFixture struct {
	hub     *tests.TestHub
	system  *core.Record
	manager *billing.Manager
	// now is the clock the sampler sees; tests move it forward themselves.
	now time.Time
}

// newTrafficFixture creates a system with a billing config that has a traffic
// limit, which is what makes the sampler write anything at all.
func newTrafficFixture(t *testing.T, limit int64) *trafficFixture {
	t.Helper()
	hub, user := tests.GetHubWithUser(t)
	t.Cleanup(hub.Cleanup)
	systemRecord, err := tests.CreateRecord(hub, "systems", map[string]any{
		"name":  "web-01",
		"host":  "127.0.0.1",
		"port":  "33914",
		"users": []string{user.Id},
	})
	require.NoError(t, err)
	_, err = tests.CreateRecord(hub, "system_billing", map[string]any{
		"system":       systemRecord.Id,
		"billingType":  billing.BillingTypePaid,
		"price":        25,
		"currency":     string(billing.CurrencyCNY),
		"cycle":        string(billing.CycleMonthly),
		"expiresAt":    time.Now().AddDate(0, 1, 0),
		"trafficLimit": limit,
		"trafficMode":  string(billing.TrafficModeSum),
		"resetDay":     14,
	})
	require.NoError(t, err)
	// Aligned to the minute so the sample clock reads cleanly in failures.
	now := time.Now().Truncate(time.Minute)
	return &trafficFixture{
		hub:     hub,
		system:  systemRecord,
		manager: hub.GetBillingManager(),
		now:     now,
	}
}

// sample feeds one agent sample with the given per-interface counters, where the
// pair is [total sent, total received] — i.e. Stats.NetworkInterfaces[2] and [3].
func (f *trafficFixture) sample(t *testing.T, counters map[string][2]uint64) {
	t.Helper()
	interfaces := make(map[string][4]uint64, len(counters))
	for name, pair := range counters {
		interfaces[name] = [4]uint64{0, 0, pair[0], pair[1]}
	}
	f.sampleRaw(t, &system.Stats{NetworkInterfaces: interfaces})
}

func (f *trafficFixture) sampleRaw(t *testing.T, stats *system.Stats) {
	t.Helper()
	f.now = f.now.Add(time.Minute)
	require.NoError(t, f.manager.AccumulateTraffic(f.hub.App, f.system.Id, stats, f.now))
}

// traffic reads the stored ledger, failing when the sampler wrote nothing.
func (f *trafficFixture) traffic(t *testing.T) *billing.Traffic {
	t.Helper()
	traffic, err := billing.LoadTraffic(f.hub.App, f.system.Id)
	require.NoError(t, err)
	require.NotNil(t, traffic, "expected a system_traffic record")
	return traffic
}

func (f *trafficFixture) billingRecord(t *testing.T) *core.Record {
	t.Helper()
	record, err := f.hub.FindFirstRecordByFilter("system_billing", "system = {:system}", dbx.Params{"system": f.system.Id})
	require.NoError(t, err)
	return record
}

func (f *trafficFixture) trafficRecord(t *testing.T) *core.Record {
	t.Helper()
	record, err := f.hub.FindFirstRecordByFilter("system_traffic", "system = {:system}", dbx.Params{"system": f.system.Id})
	require.NoError(t, err)
	return record
}

func TestAccumulateTrafficBaselineAndDeltas(t *testing.T) {
	f := newTrafficFixture(t, int64(gigabyte))

	// The first sample only establishes the baseline: bytes counted before the
	// hub started watching belong to nobody's billing period.
	f.sample(t, map[string][2]uint64{"eth0": {10 * gigabyte, 20 * gigabyte}})
	traffic := f.traffic(t)
	require.Equal(t, int64(0), traffic.UsedUpload)
	require.Equal(t, int64(0), traffic.UsedDownload)
	require.Equal(t, billing.ResetReasonInit, traffic.ResetReason)
	require.Equal(t, [2]uint64{10 * gigabyte, 20 * gigabyte}, traffic.LastCounters["eth0"])
	require.Greater(t, traffic.PeriodEnd, traffic.PeriodStart)

	// The second sample adds exactly the growth, per direction.
	f.sample(t, map[string][2]uint64{"eth0": {10*gigabyte + 1000, 20*gigabyte + 500}})
	traffic = f.traffic(t)
	require.Equal(t, int64(1000), traffic.UsedUpload)
	require.Equal(t, int64(500), traffic.UsedDownload)
	require.Equal(t, billing.ResetReasonInit, traffic.ResetReason, "no reset once the baseline exists")
}

func TestAccumulateTrafficSumsInterfaces(t *testing.T) {
	f := newTrafficFixture(t, int64(gigabyte))
	f.sample(t, map[string][2]uint64{
		"eth0": {100, 200},
		"eth1": {5, 6},
	})
	f.sample(t, map[string][2]uint64{
		"eth0": {160, 260}, // +60 / +60
		"eth1": {105, 106}, // +100 / +100
	})
	traffic := f.traffic(t)
	require.Equal(t, int64(160), traffic.UsedUpload)
	require.Equal(t, int64(160), traffic.UsedDownload)
}

func TestAccumulateTrafficCounterReset(t *testing.T) {
	f := newTrafficFixture(t, int64(gigabyte))
	f.sample(t, map[string][2]uint64{"eth0": {5 * gigabyte, 5 * gigabyte}})

	// The machine rebooted: the counters restarted from zero, so the current
	// value is what it sent since boot. It must be added, never subtracted.
	f.sample(t, map[string][2]uint64{"eth0": {300, 200}})
	traffic := f.traffic(t)
	require.Equal(t, int64(300), traffic.UsedUpload)
	require.Equal(t, int64(200), traffic.UsedDownload)
	require.Equal(t, [2]uint64{300, 200}, traffic.LastCounters["eth0"])
}

func TestAccumulateTrafficInterfaceChurn(t *testing.T) {
	f := newTrafficFixture(t, int64(gigabyte))
	f.sample(t, map[string][2]uint64{
		"eth0":   {1000, 1000},
		"veth12": {100, 100},
	})

	// A container interface disappears and a new one appears in the same
	// sample. The vanished one contributes nothing (rather than underflowing
	// the totals) and the new one only sets its baseline.
	f.sample(t, map[string][2]uint64{
		"eth0":   {1100, 1050},
		"veth99": {700, 700},
	})
	traffic := f.traffic(t)
	require.Equal(t, int64(100), traffic.UsedUpload)
	require.Equal(t, int64(50), traffic.UsedDownload)
	require.Equal(t, [2]uint64{700, 700}, traffic.LastCounters["veth99"])
	require.NotContains(t, traffic.LastCounters, "veth12")

	// The new interface counts from its second sample on.
	f.sample(t, map[string][2]uint64{
		"eth0":   {1100, 1050},
		"veth99": {800, 750},
	})
	traffic = f.traffic(t)
	require.Equal(t, int64(200), traffic.UsedUpload)
	require.Equal(t, int64(100), traffic.UsedDownload)
}

func TestAccumulateTrafficImplausibleJump(t *testing.T) {
	f := newTrafficFixture(t, int64(gigabyte))
	f.sample(t, map[string][2]uint64{"eth0": {1000, 1000}})

	// More than 10 TiB between two samples cannot be real traffic: drop it and
	// still move the baseline, so the next sample is measured from here.
	jump := uint64(10) << 40
	f.sample(t, map[string][2]uint64{"eth0": {1000 + jump + 1, 1000 + jump + 1}})
	traffic := f.traffic(t)
	require.Equal(t, int64(0), traffic.UsedUpload)
	require.Equal(t, int64(0), traffic.UsedDownload)
	require.Equal(t, [2]uint64{1000 + jump + 1, 1000 + jump + 1}, traffic.LastCounters["eth0"])

	f.sample(t, map[string][2]uint64{"eth0": {2000 + jump, 2000 + jump}})
	traffic = f.traffic(t)
	require.Equal(t, int64(999), traffic.UsedUpload, "counted again once the baseline is sane")
}

func TestAccumulateTrafficAutoRollover(t *testing.T) {
	f := newTrafficFixture(t, int64(gigabyte))
	f.sample(t, map[string][2]uint64{"eth0": {1000, 1000}})
	f.sample(t, map[string][2]uint64{"eth0": {2000, 1500}})
	require.Equal(t, int64(1000), f.traffic(t).UsedUpload)

	// Push the period end into the past: the next sample lands in a new period.
	record := f.trafficRecord(t)
	record.Set("periodEnd", f.now.Add(-time.Hour).UTC())
	require.NoError(t, f.hub.SaveNoValidate(record))

	f.sample(t, map[string][2]uint64{"eth0": {9000, 9000}})
	traffic := f.traffic(t)
	require.Equal(t, billing.ResetReasonAuto, traffic.ResetReason)
	require.Equal(t, int64(0), traffic.UsedUpload, "the rolled-over period starts empty")
	require.Greater(t, traffic.PeriodEnd, traffic.PeriodStart)
	require.False(t, f.now.Before(time.UnixMilli(traffic.PeriodStart)), "the new period contains now")

	// The first sample of the new period only re-establishes the baseline.
	f.sample(t, map[string][2]uint64{"eth0": {9500, 9400}})
	traffic = f.traffic(t)
	require.Equal(t, int64(500), traffic.UsedUpload)
	require.Equal(t, int64(400), traffic.UsedDownload)
}

func TestAccumulateTrafficAutoRolloverDropsStaleBaseline(t *testing.T) {
	f := newTrafficFixture(t, int64(gigabyte))
	f.sample(t, map[string][2]uint64{"eth0": {1000, 1000}})

	// The hub was offline across the boundary: the counter grew by a whole
	// period while nobody watched. That backlog must not be billed to the new
	// period, which starts from this sample's baseline instead.
	record := f.trafficRecord(t)
	record.Set("periodEnd", f.now.Add(-24*time.Hour).UTC())
	require.NoError(t, f.hub.SaveNoValidate(record))

	f.sample(t, map[string][2]uint64{"eth0": {500 * gigabyte, 500 * gigabyte}})
	require.Equal(t, int64(0), f.traffic(t).UsedUpload)
}

func TestAccumulateTrafficManualReset(t *testing.T) {
	f := newTrafficFixture(t, int64(gigabyte))
	f.sample(t, map[string][2]uint64{"eth0": {1000, 1000}})
	f.sample(t, map[string][2]uint64{"eth0": {3000, 2000}})
	before := f.traffic(t)
	require.Equal(t, int64(2000), before.UsedUpload)

	// The UI writes manualResetAt; the hub notices it on the next sample.
	resetAt := f.now.Add(time.Second)
	config := f.billingRecord(t)
	config.Set("manualResetAt", resetAt.UTC())
	require.NoError(t, f.hub.SaveNoValidate(config))

	f.sample(t, map[string][2]uint64{"eth0": {3100, 2100}})
	after := f.traffic(t)
	require.Equal(t, billing.ResetReasonManual, after.ResetReason)
	require.Equal(t, int64(0), after.UsedUpload, "manual reset clears the usage")
	require.Equal(t, before.PeriodStart, after.PeriodStart, "manual reset keeps the period boundaries")
	require.Equal(t, before.PeriodEnd, after.PeriodEnd)

	// manualResetAt is deliberately not cleared; lastResetAt moving past it
	// keeps the operation idempotent.
	f.sample(t, map[string][2]uint64{"eth0": {3300, 2200}})
	again := f.traffic(t)
	require.Equal(t, int64(200), again.UsedUpload)
	require.Equal(t, billing.ResetReasonManual, again.ResetReason)
}

func TestAccumulateTrafficSkipsUnlimitedAndUnconfigured(t *testing.T) {
	t.Run("no traffic limit", func(t *testing.T) {
		f := newTrafficFixture(t, 0)
		f.sample(t, map[string][2]uint64{"eth0": {1000, 1000}})
		traffic, err := billing.LoadTraffic(f.hub.App, f.system.Id)
		require.NoError(t, err)
		require.Nil(t, traffic, "an unlimited system has nothing to bill")
	})

	t.Run("no billing config", func(t *testing.T) {
		hub, user := tests.GetHubWithUser(t)
		t.Cleanup(hub.Cleanup)
		systemRecord, err := tests.CreateRecord(hub, "systems", map[string]any{
			"name":  "plain",
			"host":  "127.0.0.1",
			"port":  "33914",
			"users": []string{user.Id},
		})
		require.NoError(t, err)
		stats := &system.Stats{NetworkInterfaces: map[string][4]uint64{"eth0": {0, 0, 5, 5}}}
		require.NoError(t, hub.GetBillingManager().AccumulateTraffic(hub.App, systemRecord.Id, stats, time.Now()))
		traffic, err := billing.LoadTraffic(hub.App, systemRecord.Id)
		require.NoError(t, err)
		require.Nil(t, traffic)
	})

	t.Run("agent reports no interfaces", func(t *testing.T) {
		f := newTrafficFixture(t, int64(gigabyte))
		f.sampleRaw(t, &system.Stats{})
		traffic, err := billing.LoadTraffic(f.hub.App, f.system.Id)
		require.NoError(t, err)
		require.Nil(t, traffic, "without counters there is no baseline to create")
	})
}
