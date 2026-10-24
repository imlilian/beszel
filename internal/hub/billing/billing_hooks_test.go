package billing

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

// newBillingRecord builds an unsaved record over a throwaway collection with
// the same field names as the real one, so the hooks can be exercised without a
// database.
func newBillingRecord() *core.Record {
	collection := core.NewBaseCollection("system_billing_test")
	collection.Fields.Add(
		&core.SelectField{Name: "billingType", Values: []string{"free", "paid"}, MaxSelect: 1},
		&core.SelectField{Name: "currency", Values: []string{"CNY", "USD", "EUR", "GBP"}, MaxSelect: 1},
		&core.SelectField{
			Name:      "cycle",
			Values:    []string{"monthly", "semiannual", "annual", "biennial", "triennial", "permanent"},
			MaxSelect: 1,
		},
		&core.SelectField{Name: "trafficMode", Values: []string{"sum", "upload"}, MaxSelect: 1},
		&core.NumberField{Name: "price"},
		&core.NumberField{Name: "notifyDaysBefore"},
		&core.NumberField{Name: "resetDay"},
		&core.DateField{Name: "paidAt"},
		&core.DateField{Name: "expiresAt"},
	)
	return core.NewRecord(collection)
}

func TestApplyDefaults(t *testing.T) {
	record := newBillingRecord()
	applyDefaults(record)

	require.Equal(t, BillingTypePaid, record.GetString("billingType"))
	require.Equal(t, string(CurrencyCNY), record.GetString("currency"))
	require.Equal(t, string(CycleMonthly), record.GetString("cycle"))
	require.Equal(t, string(TrafficModeSum), record.GetString("trafficMode"))
	require.Equal(t, DefaultNotifyDaysBefore, record.GetInt("notifyDaysBefore"))
	require.Equal(t, DefaultResetDay, record.GetInt("resetDay"))

	// Explicit values survive.
	explicit := newBillingRecord()
	explicit.Set("billingType", BillingTypeFree)
	explicit.Set("currency", string(CurrencyUSD))
	explicit.Set("notifyDaysBefore", 3)
	explicit.Set("resetDay", 14)
	applyDefaults(explicit)
	require.Equal(t, BillingTypeFree, explicit.GetString("billingType"))
	require.Equal(t, string(CurrencyUSD), explicit.GetString("currency"))
	require.Equal(t, 3, explicit.GetInt("notifyDaysBefore"))
	require.Equal(t, 14, explicit.GetInt("resetDay"))
}

func TestNormalize(t *testing.T) {
	paidAt := time.Date(2026, time.February, 14, 12, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.UTC)

	t.Run("paid without an expiry is rejected", func(t *testing.T) {
		record := newBillingRecord()
		applyDefaults(record)
		record.Set("price", 25.0)
		require.Error(t, normalize(record))
	})

	t.Run("paid with a valid expiry passes", func(t *testing.T) {
		record := newBillingRecord()
		applyDefaults(record)
		record.Set("price", 25.0)
		record.Set("paidAt", paidAt)
		record.Set("expiresAt", expiresAt)
		require.NoError(t, normalize(record))
		require.Equal(t, 25.0, record.GetFloat("price"))
	})

	t.Run("expiry must be after the payment date", func(t *testing.T) {
		record := newBillingRecord()
		applyDefaults(record)
		record.Set("paidAt", expiresAt)
		record.Set("expiresAt", paidAt)
		require.Error(t, normalize(record))
	})

	t.Run("permanent clears the expiry", func(t *testing.T) {
		record := newBillingRecord()
		applyDefaults(record)
		record.Set("cycle", string(CyclePermanent))
		record.Set("expiresAt", expiresAt)
		require.NoError(t, normalize(record))
		require.True(t, record.GetDateTime("expiresAt").IsZero())
	})

	t.Run("free clears the cost fields", func(t *testing.T) {
		record := newBillingRecord()
		applyDefaults(record)
		record.Set("billingType", BillingTypeFree)
		record.Set("price", 25.0)
		record.Set("paidAt", paidAt)
		record.Set("expiresAt", expiresAt)
		require.NoError(t, normalize(record))
		require.Equal(t, 0.0, record.GetFloat("price"))
		require.True(t, record.GetDateTime("paidAt").IsZero())
		require.True(t, record.GetDateTime("expiresAt").IsZero())
	})

	t.Run("unknown billing type is rejected", func(t *testing.T) {
		record := newBillingRecord()
		require.Error(t, normalize(record))
	})
}
