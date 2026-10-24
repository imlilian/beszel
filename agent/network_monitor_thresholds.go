package agent

import (
	"strconv"

	"github.com/henrygd/beszel/agent/utils"
)

// Monitor loss threshold.
//
// A probe that completes but takes longer than this is counted as packet loss
// instead of a very slow success. Normalizing at the sample level (see
// classifyProbe) keeps one definition of loss across everything derived from
// history: loss percentage, avg/min/max, stored network_monitor_stats rows and
// the loss alert. Styling slow samples red in the UI only would leave those
// values disagreeing with each other.
const defaultLossThresholdUs int64 = 900_000 // 900ms

// lossThresholdUs returns the response time in microseconds at or above which a
// successful probe counts as loss. MONITOR_LOSS_THRESHOLD_MS overrides the
// default; a value of 0 or less disables the conversion, restoring the
// "only transport errors are loss" behavior.
//
// Read on each use so the value can be changed without a rebuild and so tests
// can set it per case. A single env lookup per probe (at most once per monitor
// interval) is negligible.
func lossThresholdUs() int64 {
	if raw, ok := utils.GetEnv("MONITOR_LOSS_THRESHOLD_MS"); ok {
		ms, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return defaultLossThresholdUs
		}
		if ms <= 0 {
			return 0
		}
		return ms * 1000
	}
	return defaultLossThresholdUs
}

// probeOutcome classifies one probe attempt before it is folded into history.
type probeOutcome uint8

const (
	// probeOK is a successful probe that stays under the loss threshold.
	probeOK probeOutcome = iota
	// probeFailed is a transport error, timeout or resolution failure.
	probeFailed
	// probeSlow is a successful probe that exceeded the loss threshold.
	probeSlow
)

// classifyProbe converts a raw probe result into the microsecond value recorded
// in history. Loss is encoded as -1, matching monitorAggregate.addResponse.
func classifyProbe(responseUs int64, probeErr error, thresholdUs int64) (int64, probeOutcome) {
	if probeErr != nil {
		return -1, probeFailed
	}
	if thresholdUs > 0 && responseUs >= thresholdUs {
		return -1, probeSlow
	}
	return responseUs, probeOK
}
