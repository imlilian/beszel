package billing

import (
	"database/sql"
	"errors"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Values stored in `system_traffic.resetReason`.
const (
	ResetReasonInit   = "init"   // first sample for this system
	ResetReasonAuto   = "auto"   // the billing period rolled over
	ResetReasonManual = "manual" // a user pressed "reset now"
)

// maxTrafficDelta is the largest believable increase between two samples. A
// 60s sample cannot move 10 TiB on real hardware, so anything larger is a driver
// bug or an interface swap that reset the counters to a bogus value.
const maxTrafficDelta uint64 = 10 << 40 // 10 TiB

// AccumulateTraffic adds the bytes transferred since the previous sample to the
// current period, creating the `system_traffic` record on first use.
//
// It is called from the system update transaction, right after the sample is
// stored, and reads the agents' OS interface counters (`Stats.NetworkInterfaces`
// [2] and [3], cumulative bytes sent / received). Three properties follow from
// diffing cumulative counters instead of summing per-sample rates:
//
//   - a hub that was offline for hours still accounts the whole gap, because the
//     counters kept running on the agent (as long as the machine did not reboot);
//   - an agent or hub restart changes nothing, the counters live in the kernel;
//   - a machine reboot (counters back to zero) is detected by a decreasing value
//     and only costs the < 60s between the last sample and the reboot.
//
// Systems without a traffic limit are skipped: the record is a quota ledger, and
// an unlimited system has nothing to bill against.
func (bm *Manager) AccumulateTraffic(txApp core.App, systemID string, stats *system.Stats, now time.Time) error {
	// Old agents don't report interface counters at all. Do not create a record
	// with an empty baseline: the next sample would then look like a huge jump.
	if stats == nil || len(stats.NetworkInterfaces) == 0 {
		return nil
	}
	cfgRecord, err := LoadRecord(txApp, systemID)
	if err != nil {
		return err
	}
	if cfgRecord == nil {
		return nil
	}
	cfg := configFromRecord(cfgRecord)
	if cfg.TrafficLimit <= 0 {
		return nil
	}
	record, traffic, err := loadTrafficRecord(txApp, systemID)
	if err != nil {
		return err
	}
	if traffic == nil {
		traffic = &Traffic{SystemId: systemID}
	}

	// 1) Period rollover / manual reset.
	//
	// `manualResetAt` is written by the UI and never cleared: comparing it with
	// `lastResetAt` keeps the operation idempotent, so a repeated sample can't
	// reset the same period twice. When the period has ended as well, the auto
	// rollover wins because the boundaries have to move anyway.
	switch reason := resetReason(cfg, traffic, now); reason {
	case "":
		// Keep counting in the current period.
	default:
		if reason != ResetReasonManual {
			start, end := Period(now, cfg.ResetDay, time.Local)
			traffic.PeriodStart, traffic.PeriodEnd = start.UnixMilli(), end.UnixMilli()
		}
		traffic.UsedUpload, traffic.UsedDownload = 0, 0
		traffic.LastResetAt = now.UnixMilli()
		traffic.ResetReason = reason
		// Drop the baselines so this sample only re-establishes them; carrying
		// them across the boundary would add bytes that belong to the previous
		// period, or (after a long gap) a whole period's worth.
		traffic.LastCounters = make(map[string][2]uint64, len(stats.NetworkInterfaces))
		// A fresh period gets a fresh reminder ladder. Without this the 80% band
		// of the previous period would block this period's.
		if cfg.TrafficRemindLevel != LevelNone {
			cfgRecord.Set("trafficRemindLevel", LevelNone)
			if err := txApp.SaveNoValidate(cfgRecord); err != nil {
				return err
			}
		}
	}

	// 2) Diff every interface against its own previous reading.
	//
	// Per interface instead of on the totals: interfaces come and go (docker,
	// veth, VPN), and subtracting totals would underflow the moment one
	// disappears. A newly seen interface only contributes a baseline.
	counters := make(map[string][2]uint64, len(stats.NetworkInterfaces))
	var upDelta, downDelta uint64
	var dropped int
	for name, values := range stats.NetworkInterfaces {
		current := [2]uint64{values[2], values[3]}
		if previous, seen := traffic.LastCounters[name]; seen {
			up, upDropped := safeDelta(previous[0], current[0])
			down, downDropped := safeDelta(previous[1], current[1])
			upDelta += up
			downDelta += down
			if upDropped || downDropped {
				dropped++
			}
		}
		counters[name] = current
	}
	if dropped > 0 && bm != nil && bm.app != nil {
		bm.app.Logger().Warn("Discarded implausible traffic delta", "system", systemID, "interfaces", dropped)
	}

	traffic.LastCounters = counters
	traffic.UsedUpload += int64(upDelta)
	traffic.UsedDownload += int64(downDelta)
	traffic.LastSampleAt = now.UnixMilli()

	return saveTraffic(txApp, record, traffic)
}

// resetReason reports why the counters have to restart, or "" to keep counting.
// `init` (no record yet) always wins because there is nothing to preserve.
func resetReason(cfg *Config, traffic *Traffic, now time.Time) string {
	if traffic == nil || traffic.PeriodEnd <= 0 {
		return ResetReasonInit
	}
	if !now.Before(time.UnixMilli(traffic.PeriodEnd)) {
		return ResetReasonAuto
	}
	if cfg.ManualResetAt > traffic.LastResetAt {
		return ResetReasonManual
	}
	return ""
}

// safeDelta returns the growth of one counter since the previous sample.
//
// A counter that went backwards means the machine rebooted: the current value is
// what it has sent since boot, so that is the delta. A jump beyond
// maxTrafficDelta is not a real transfer (30 days of 100 Gbit/s is ~32 TB, so
// even a long hub outage cannot reach 10 TiB of *increase* on a normal link);
// it is dropped and reported.
func safeDelta(previous, current uint64) (delta uint64, dropped bool) {
	if current >= previous {
		grown := current - previous
		if grown > maxTrafficDelta {
			return 0, true
		}
		return grown, false
	}
	return current, false
}

// loadTrafficRecord returns the record and its typed view, allocating an unsaved
// record when the system has none yet. Editing the record in place (instead of
// deleting and recreating) keeps the `created` timestamp and the unique index
// happy.
func loadTrafficRecord(app core.App, systemID string) (*core.Record, *Traffic, error) {
	record, err := app.FindFirstRecordByFilter(
		"system_traffic",
		"system = {:system}",
		dbx.Params{"system": systemID},
	)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
		collection, err := app.FindCachedCollectionByNameOrId("system_traffic")
		if err != nil {
			return nil, nil, err
		}
		record := core.NewRecord(collection)
		record.Set("system", systemID)
		return record, nil, nil
	}
	return record, trafficFromRecord(record), nil
}

// saveTraffic writes the typed view back onto its record. Validation is skipped
// because the caller already holds the system's billing config, so the relation
// and every field are known good; the hot path runs once per system per minute.
func saveTraffic(app core.App, record *core.Record, traffic *Traffic) error {
	record.Set("periodStart", millisToTime(traffic.PeriodStart))
	record.Set("periodEnd", millisToTime(traffic.PeriodEnd))
	record.Set("usedUpload", traffic.UsedUpload)
	record.Set("usedDownload", traffic.UsedDownload)
	record.Set("lastCounters", traffic.LastCounters)
	record.Set("lastSampleAt", millisToTime(traffic.LastSampleAt))
	record.Set("lastResetAt", millisToTime(traffic.LastResetAt))
	record.Set("resetReason", traffic.ResetReason)
	return app.SaveNoValidate(record)
}

/** Zero is PocketBase's "unset" date, so it has to stay the zero time. */
func millisToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
