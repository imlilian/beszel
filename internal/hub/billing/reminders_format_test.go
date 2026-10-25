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
