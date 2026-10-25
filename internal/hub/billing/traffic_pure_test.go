package billing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSafeDelta(t *testing.T) {
	cases := []struct {
		name     string
		previous uint64
		current  uint64
		delta    uint64
		dropped  bool
	}{
		{name: "normal growth", previous: 1000, current: 1500, delta: 500},
		{name: "no traffic between samples", previous: 1500, current: 1500, delta: 0},
		{name: "counter went back to zero after a reboot", previous: 5_000_000, current: 800, delta: 800},
		{name: "exactly the ceiling is still believable", previous: 0, current: maxTrafficDelta, delta: maxTrafficDelta},
		{name: "beyond the ceiling is a driver bug", previous: 10, current: maxTrafficDelta + 11, delta: 0, dropped: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			delta, dropped := safeDelta(c.previous, c.current)
			require.Equal(t, c.delta, delta)
			require.Equal(t, c.dropped, dropped)
		})
	}
}

func TestResetReason(t *testing.T) {
	now := date(2026, time.March, 20)
	live := &Traffic{PeriodEnd: date(2026, time.March, 25).UnixMilli(), LastResetAt: now.Add(-time.Hour).UnixMilli()}

	require.Equal(t, ResetReasonInit, resetReason(&Config{}, nil, now), "no record yet")
	require.Equal(t, ResetReasonInit, resetReason(&Config{}, &Traffic{}, now), "no period yet")
	require.Equal(t, "", resetReason(&Config{}, live, now))
	require.Equal(t, ResetReasonAuto, resetReason(&Config{}, &Traffic{PeriodEnd: now.UnixMilli()}, now),
		"the period end itself already belongs to the new period")
	require.Equal(t, ResetReasonManual, resetReason(&Config{ManualResetAt: now.UnixMilli()}, live, now))
	require.Equal(t, ResetReasonAuto, resetReason(&Config{ManualResetAt: now.UnixMilli()}, &Traffic{PeriodEnd: now.Add(-time.Minute).UnixMilli()}, now),
		"a due rollover wins over a pending manual reset")
	require.Equal(t, "", resetReason(&Config{ManualResetAt: live.LastResetAt}, live, now),
		"a manual reset already applied in this period does not fire again")
}
