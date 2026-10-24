package billing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

func TestPeriod(t *testing.T) {
	utc := time.UTC
	cases := []struct {
		name  string
		now   time.Time
		reset int
		start time.Time
		end   time.Time
	}{
		{
			name:  "mid period borrows the previous month's reset",
			now:   date(2026, time.March, 5),
			reset: 14,
			start: time.Date(2026, time.February, 14, 0, 0, 0, 0, utc),
			end:   time.Date(2026, time.March, 14, 0, 0, 0, 0, utc),
		},
		{
			name:  "reset day itself starts a new period",
			now:   time.Date(2026, time.March, 14, 0, 0, 0, 0, utc),
			reset: 14,
			start: time.Date(2026, time.March, 14, 0, 0, 0, 0, utc),
			end:   time.Date(2026, time.April, 14, 0, 0, 0, 0, utc),
		},
		{
			name:  "one minute before the reset still belongs to the old period",
			now:   time.Date(2026, time.March, 13, 23, 59, 0, 0, utc),
			reset: 14,
			start: time.Date(2026, time.February, 14, 0, 0, 0, 0, utc),
			end:   time.Date(2026, time.March, 14, 0, 0, 0, 0, utc),
		},
		{
			name:  "day 31 clamps to February",
			now:   date(2026, time.February, 10),
			reset: 31,
			start: time.Date(2026, time.January, 31, 0, 0, 0, 0, utc),
			end:   time.Date(2026, time.February, 28, 0, 0, 0, 0, utc),
		},
		{
			name:  "February 28 is the clamped reset day",
			now:   date(2026, time.February, 28),
			reset: 31,
			start: time.Date(2026, time.February, 28, 0, 0, 0, 0, utc),
			end:   time.Date(2026, time.March, 31, 0, 0, 0, 0, utc),
		},
		{
			name:  "leap year clamps to February 29",
			now:   date(2024, time.February, 29),
			reset: 31,
			start: time.Date(2024, time.February, 29, 0, 0, 0, 0, utc),
			end:   time.Date(2024, time.March, 31, 0, 0, 0, 0, utc),
		},
		{
			name:  "January borrows the previous year's December",
			now:   date(2026, time.January, 15),
			reset: 31,
			start: time.Date(2025, time.December, 31, 0, 0, 0, 0, utc),
			end:   time.Date(2026, time.January, 31, 0, 0, 0, 0, utc),
		},
		{
			name:  "reset day below one falls back to the first",
			now:   date(2026, time.March, 5),
			reset: 0,
			start: time.Date(2026, time.March, 1, 0, 0, 0, 0, utc),
			end:   time.Date(2026, time.April, 1, 0, 0, 0, 0, utc),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := Period(tc.now, tc.reset, utc)
			require.True(t, tc.start.Equal(start), "start: want %s, got %s", tc.start, start)
			require.True(t, tc.end.Equal(end), "end: want %s, got %s", tc.end, end)
			require.True(t, start.Before(end))
			// now must always be inside [start, end)
			require.False(t, tc.now.Before(start))
			require.True(t, tc.now.Before(end))
		})
	}
}

func TestPeriodUsesGivenLocation(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// 2026-03-14 00:30 in Shanghai is still 2026-03-13 16:30 UTC.
	now := time.Date(2026, time.March, 14, 0, 30, 0, 0, shanghai)
	start, end := Period(now, 14, shanghai)
	require.True(t, time.Date(2026, time.March, 14, 0, 0, 0, 0, shanghai).Equal(start))
	require.True(t, time.Date(2026, time.April, 14, 0, 0, 0, 0, shanghai).Equal(end))
}

func TestReminderLevel(t *testing.T) {
	cases := []struct {
		daysLeft int
		notify   int
		want     int
	}{
		{7, 7, 7},
		{6, 7, 7},
		{4, 7, 7},
		{3, 7, 3},
		{2, 7, 3},
		{1, 7, 1},
		{0, 7, -1},
		{-5, 7, -1},
		{8, 7, 0},
		{30, 30, 30},
		{2, 2, 2},
		{5, 0, 0}, // advance notice disabled: only the expired band remains
		{1, 0, 0},
		{0, 0, -1},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, ReminderLevel(tc.daysLeft, tc.notify), "daysLeft=%d notify=%d", tc.daysLeft, tc.notify)
	}
}

func TestDaysUntilExpiry(t *testing.T) {
	utc := time.UTC
	// 23:30 today vs 00:30 tomorrow is one whole day, not zero.
	now := time.Date(2026, time.March, 14, 23, 30, 0, 0, utc)
	tomorrow := time.Date(2026, time.March, 15, 0, 30, 0, 0, utc)
	require.Equal(t, 1, DaysUntilExpiry(tomorrow.UnixMilli(), now))

	earlierToday := time.Date(2026, time.March, 14, 6, 0, 0, 0, utc)
	require.Equal(t, 0, DaysUntilExpiry(earlierToday.UnixMilli(), now))

	threeDaysAgo := time.Date(2026, time.March, 11, 18, 0, 0, 0, utc)
	require.Equal(t, -3, DaysUntilExpiry(threeDaysAgo.UnixMilli(), now))

	// A zero expiry means "no expiry"; callers must check it first.
	require.Equal(t, 0, DaysUntilExpiry(0, now))
}

func TestAddCycle(t *testing.T) {
	base := time.Date(2026, time.January, 14, 9, 30, 0, 0, time.UTC)
	cases := []struct {
		cycle Cycle
		want  time.Time
	}{
		{CycleMonthly, time.Date(2026, time.February, 14, 9, 30, 0, 0, time.UTC)},
		{CycleSemiannual, time.Date(2026, time.July, 14, 9, 30, 0, 0, time.UTC)},
		{CycleAnnual, time.Date(2027, time.January, 14, 9, 30, 0, 0, time.UTC)},
		{CycleBiennial, time.Date(2028, time.January, 14, 9, 30, 0, 0, time.UTC)},
		{CycleTriennial, time.Date(2029, time.January, 14, 9, 30, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		require.True(t, tc.want.Equal(AddCycle(base, tc.cycle)), "cycle %s", tc.cycle)
	}

	// Day-of-month is clamped instead of overflowing into the next month.
	require.True(t,
		time.Date(2026, time.February, 28, 9, 30, 0, 0, time.UTC).
			Equal(AddCycle(time.Date(2026, time.January, 31, 9, 30, 0, 0, time.UTC), CycleMonthly)),
	)
	require.True(t,
		time.Date(2024, time.February, 29, 9, 30, 0, 0, time.UTC).
			Equal(AddCycle(time.Date(2024, time.January, 31, 9, 30, 0, 0, time.UTC), CycleMonthly)),
	)

	// No expiry for permanent (or unknown) cycles.
	require.True(t, AddCycle(base, CyclePermanent).IsZero())
	require.True(t, AddCycle(base, Cycle("")).IsZero())
}

func TestParseCounters(t *testing.T) {
	parsed := ParseCounters(map[string]any{
		"eth0":  []any{float64(1024), float64(2048)},
		"ens3":  []any{int64(7), int64(9)},
		"bad":   []any{float64(1)},
		"worse": "nope",
		"neg":   []any{float64(-5), float64(3)},
	})
	require.Equal(t, [2]uint64{1024, 2048}, parsed["eth0"])
	require.Equal(t, [2]uint64{7, 9}, parsed["ens3"])
	require.Equal(t, [2]uint64{0, 3}, parsed["neg"])
	require.NotContains(t, parsed, "bad")
	require.NotContains(t, parsed, "worse")

	require.Empty(t, ParseCounters(nil))
	require.Empty(t, ParseCounters("{}"))
}

func TestConfigHasExpiry(t *testing.T) {
	require.True(t, (&Config{BillingType: BillingTypePaid, Cycle: CycleMonthly, ExpiresAt: 1}).HasExpiry())
	require.False(t, (&Config{BillingType: BillingTypePaid, Cycle: CyclePermanent, ExpiresAt: 1}).HasExpiry())
	require.False(t, (&Config{BillingType: BillingTypeFree, Cycle: CycleMonthly, ExpiresAt: 1}).HasExpiry())
	require.False(t, (&Config{BillingType: BillingTypePaid, Cycle: CycleMonthly}).HasExpiry())
}
