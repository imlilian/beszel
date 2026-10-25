package billing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCostLabel(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"monthly in CNY", Config{Price: 25, Currency: CurrencyCNY, Cycle: CycleMonthly}, "¥25/mo"},
		{"annual in USD", Config{Price: 120, Currency: CurrencyUSD, Cycle: CycleAnnual}, "$120/yr"},
		{"fractional price keeps its decimals", Config{Price: 9.99, Currency: CurrencyEUR, Cycle: CycleSemiannual}, "€9.99/6mo"},
		{"no price", Config{Currency: CurrencyCNY, Cycle: CycleMonthly}, ""},
		{"unknown currency falls back to the code", Config{Price: 5, Currency: "JPY", Cycle: CycleMonthly}, "JPY 5/mo"},
		{"unknown cycle has no suffix", Config{Price: 5, Currency: CurrencyGBP, Cycle: "weekly"}, "£5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, costLabel(&tc.cfg))
		})
	}
}

func TestExpiryMessage(t *testing.T) {
	now := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.UTC)
	expires := func(year int, month time.Month, day int) int64 {
		return time.Date(year, month, day, 9, 0, 0, 0, time.UTC).UnixMilli()
	}
	cfg := &Config{Price: 25, Currency: CurrencyCNY, Cycle: CycleMonthly}

	t.Run("upcoming expiry counts the days left", func(t *testing.T) {
		cfg.ExpiresAt = expires(2026, time.March, 21)
		title, message := expiryMessage("web-01", cfg, 7, now)
		require.Equal(t, "System expires in 7 days: web-01", title)
		require.Equal(t, "web-01 (¥25/mo) expires on 2026-03-21 (7 days left). Update the expiry date after renewing.", message)
	})

	t.Run("one day left is singular", func(t *testing.T) {
		cfg.ExpiresAt = expires(2026, time.March, 15)
		title, message := expiryMessage("web-01", cfg, 1, now)
		require.Equal(t, "System expires in 1 day: web-01", title)
		require.Contains(t, message, "(1 day left)")
	})

	t.Run("expiry day", func(t *testing.T) {
		cfg.ExpiresAt = expires(2026, time.March, 14)
		title, message := expiryMessage("web-01", cfg, 0, now)
		require.Equal(t, "System expires today: web-01", title)
		require.Equal(t, "web-01 (¥25/mo) expires today (2026-03-14). Update the expiry date after renewing.", message)
	})

	t.Run("expired", func(t *testing.T) {
		cfg.ExpiresAt = expires(2026, time.March, 11)
		title, message := expiryMessage("web-01", cfg, -3, now)
		require.Equal(t, "System expired: web-01", title)
		require.Equal(t, "web-01 (¥25/mo) expired on 2026-03-11 (3 days ago). Update the expiry date after renewing.", message)
	})

	t.Run("expired one day ago is singular", func(t *testing.T) {
		cfg.ExpiresAt = expires(2026, time.March, 13)
		_, message := expiryMessage("web-01", cfg, -1, now)
		require.Contains(t, message, "(1 day ago)")
	})

	t.Run("a config without a price omits the cost", func(t *testing.T) {
		noPrice := &Config{Currency: CurrencyCNY, Cycle: CycleMonthly, ExpiresAt: expires(2026, time.March, 21)}
		_, message := expiryMessage("web-01", noPrice, 7, now)
		require.Contains(t, message, "web-01 expires on 2026-03-21")
	})
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:                  "0 B",
		512:                "512 B",
		1024:               "1 KB",
		1536:               "1.5 KB",
		1024 * 1024:        "1 MB",
		1024 * 1024 * 1024: "1 GB",
		820 * (1 << 30):    "820 GB",
		int64(1) << 40:     "1 TB",
		1200 * (1 << 30):   "1.2 TB",
		int64(1) << 50:     "1 PB",
	}
	for bytes, want := range cases {
		require.Equal(t, want, formatBytes(bytes), "bytes=%d", bytes)
	}
}

func TestTrafficMessage(t *testing.T) {
	now := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.UTC)
	// 18 days from the 14th, so the countdown reads 18 in the assertions below.
	end := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	traffic := &Traffic{PeriodEnd: end.UnixMilli()}
	cfg := &Config{TrafficLimit: 1 << 40}

	t.Run("80% band", func(t *testing.T) {
		title, message := trafficMessage("web-01", cfg, traffic, 820*(1<<30), 80.07, now)
		require.Equal(t, "Traffic at 80%: web-01", title)
		require.Equal(t, "web-01 has used 820 GB of 1 TB (80%) this period, resets in 18 days.", message)
	})

	t.Run("over the quota", func(t *testing.T) {
		title, message := trafficMessage("web-01", cfg, traffic, 1200*(1<<30), 117.2, now)
		require.Equal(t, "Traffic quota exceeded: web-01", title)
		require.Equal(t,
			"web-01 has used 1.2 TB of 1 TB (117%) this period, resets in 18 days. Further traffic may be billed by your provider.",
			message)
	})

	t.Run("the last day of the period", func(t *testing.T) {
		lastDay := &Traffic{PeriodEnd: now.UnixMilli()}
		_, message := trafficMessage("web-01", cfg, lastDay, 900*(1<<30), 87.9, now)
		require.Equal(t, "web-01 has used 900 GB of 1 TB (88%) this period, resets today.", message)
	})

	t.Run("a period without an end still reads fine", func(t *testing.T) {
		_, message := trafficMessage("web-01", cfg, &Traffic{}, 900*(1<<30), 87.9, now)
		require.Equal(t, "web-01 has used 900 GB of 1 TB (88%) this period.", message)
	})
}
