package agent

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLossThresholdUs(t *testing.T) {
	cases := []struct {
		name string
		env  string
		set  bool
		want int64
	}{
		{name: "default when unset", want: defaultLossThresholdUs},
		{name: "explicit 900ms", env: "900", set: true, want: 900_000},
		{name: "explicit 1500ms", env: "1500", set: true, want: 1_500_000},
		{name: "zero disables", env: "0", set: true, want: 0},
		{name: "negative disables", env: "-5", set: true, want: 0},
		{name: "unparsable falls back to default", env: "abc", set: true, want: defaultLossThresholdUs},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("MONITOR_LOSS_THRESHOLD_MS", tc.env)
			}
			assert.Equal(t, tc.want, lossThresholdUs())
		})
	}
}

func TestClassifyProbe(t *testing.T) {
	errBoom := errors.New("boom")
	cases := []struct {
		name       string
		responseUs int64
		err        error
		threshold  int64
		wantUs     int64
		wantKind   probeOutcome
	}{
		{name: "fast success", responseUs: 42_000, threshold: 900_000, wantUs: 42_000, wantKind: probeOK},
		{name: "just under threshold", responseUs: 899_999, threshold: 900_000, wantUs: 899_999, wantKind: probeOK},
		{name: "exactly at threshold counts as loss", responseUs: 900_000, threshold: 900_000, wantUs: -1, wantKind: probeSlow},
		{name: "well over threshold", responseUs: 2_500_000, threshold: 900_000, wantUs: -1, wantKind: probeSlow},
		{name: "transport error stays loss", responseUs: 10_000, err: errBoom, threshold: 900_000, wantUs: -1, wantKind: probeFailed},
		{name: "threshold disabled keeps slow values", responseUs: 2_500_000, threshold: 0, wantUs: 2_500_000, wantKind: probeOK},
		{name: "negative threshold keeps slow values", responseUs: 2_500_000, threshold: -1, wantUs: 2_500_000, wantKind: probeOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotUs, gotKind := classifyProbe(tc.responseUs, tc.err, tc.threshold)
			assert.Equal(t, tc.wantUs, gotUs)
			assert.Equal(t, tc.wantKind, gotKind)
		})
	}
}

// The normalized sample must move loss percentage, average and the stored
// counts together, otherwise charts and alerts would disagree with each other.
func TestSlowProbeCountsAsLossInHistory(t *testing.T) {
	t.Setenv("MONITOR_LOSS_THRESHOLD_MS", "900")
	threshold := lossThresholdUs()
	require.EqualValues(t, 900_000, threshold)

	history := newMonitorHistory()
	now := time.Now()

	// Two fast successes and one slow-but-successful probe in the same window.
	for i, raw := range []int64{40_000, 60_000, 1_200_000} {
		responseUs, _ := classifyProbe(raw, nil, threshold)
		history.record(monitorSample{responseUs: responseUs, timestamp: now.Add(time.Duration(i) * time.Second)})
	}

	result, ok := history.result(time.Minute, now.Add(5*time.Second))
	require.True(t, ok)

	assert.EqualValues(t, 3, result.TotalCount)
	assert.EqualValues(t, 2, result.SuccessCount, "the slow probe must not count as a success")
	assert.EqualValues(t, 100_000, result.ResponseSum, "only the two fast probes contribute")
	assert.EqualValues(t, 50_000, result.AvgResponse)
	assert.EqualValues(t, 40_000, result.MinResponse)
	assert.EqualValues(t, 60_000, result.MaxResponse)
	assert.Equal(t, 33.33, result.PacketLoss)
}

// Disabling the threshold must restore the original semantics for slow samples.
func TestSlowProbeCountsAsSuccessWhenThresholdDisabled(t *testing.T) {
	threshold := int64(0)
	history := newMonitorHistory()
	now := time.Now()

	for i, raw := range []int64{40_000, 1_200_000} {
		responseUs, _ := classifyProbe(raw, nil, threshold)
		history.record(monitorSample{responseUs: responseUs, timestamp: now.Add(time.Duration(i) * time.Second)})
	}

	result, ok := history.result(time.Minute, now.Add(5*time.Second))
	require.True(t, ok)
	assert.EqualValues(t, 2, result.SuccessCount)
	assert.EqualValues(t, 1_240_000, result.ResponseSum)
	assert.Zero(t, result.PacketLoss)
	assert.EqualValues(t, 1_200_000, result.MaxResponse)
}
