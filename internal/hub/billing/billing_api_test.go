//go:build testing

package billing_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/henrygd/beszel/internal/tests"
	pbtests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

// PRD 8: billing is member-editable, traffic is read-only for everyone, and
// readonly users can never write either collection.
func TestBillingAPIRules(t *testing.T) {
	hub, member := tests.GetHubWithUser(t)
	defer hub.Cleanup()

	memberToken, err := member.NewAuthToken()
	require.NoError(t, err)

	readonly, err := tests.CreateUserWithRole(hub, "readonly@example.com", "password123", "readonly")
	require.NoError(t, err)
	readonlyToken, err := readonly.NewAuthToken()
	require.NoError(t, err)

	outsider, err := tests.CreateUser(hub, "outsider@example.com", "password123")
	require.NoError(t, err)
	outsiderToken, err := outsider.NewAuthToken()
	require.NoError(t, err)

	// One system per create scenario: the billing collection is 1:1 with systems,
	// so reusing one would fail on the unique index instead of on the rules.
	newSystem := func(t *testing.T, name string) string {
		record, err := tests.CreateRecord(hub, "systems", map[string]any{
			"name":  name,
			"users": []string{member.Id, readonly.Id},
			"host":  "127.0.0.1",
		})
		require.NoError(t, err)
		return record.Id
	}
	existingSystem := newSystem(t, "billing-existing")
	memberSystem := newSystem(t, "billing-member")
	paidNoExpirySystem := newSystem(t, "billing-paid-no-expiry")
	readonlySystem := newSystem(t, "billing-readonly")

	// Created through the app (not the API) so the PATCH scenarios have a target.
	existing, err := tests.CreateRecord(hub, "system_billing", map[string]any{
		"system":      existingSystem,
		"billingType": "paid",
		"cycle":       "monthly",
		"expiresAt":   "2026-03-14 00:00:00.000Z",
	})
	require.NoError(t, err)

	// Pretend the reminder cron already walked down to the 7-day band.
	existing.Set("remindLevel", 7)
	require.NoError(t, hub.Save(existing))

	auth := func(token string) map[string]string {
		return map[string]string{"Authorization": token}
	}

	scenarios := []tests.ApiScenario{
		{
			Name:    "member can create billing config",
			Method:  http.MethodPost,
			URL:     "/api/collections/system_billing/records",
			Body:    strings.NewReader(`{"system":"` + memberSystem + `","billingType":"free","price":99}`),
			Headers: auth(memberToken),
			// The hook clears the cost fields for a free system.
			ExpectedStatus:  200,
			ExpectedContent: []string{`"billingType":"free"`, `"price":0`},
		},
		{
			Name:            "paid without an expiry is rejected",
			Method:          http.MethodPost,
			URL:             "/api/collections/system_billing/records",
			Body:            strings.NewReader(`{"system":"` + paidNoExpirySystem + `","billingType":"paid","cycle":"monthly"}`),
			Headers:         auth(memberToken),
			ExpectedStatus:  400,
			ExpectedContent: []string{"is required for a paid system"},
		},
		{
			// PocketBase surfaces a failed create rule as 400 (validation), while a
			// nil rule ("nobody") is 403. Either way the write is refused.
			Name:            "readonly cannot create billing config",
			Method:          http.MethodPost,
			URL:             "/api/collections/system_billing/records",
			Body:            strings.NewReader(`{"system":"` + readonlySystem + `","billingType":"free"}`),
			Headers:         auth(readonlyToken),
			ExpectedStatus:  400,
			ExpectedContent: []string{"Failed to create record"},
		},
		{
			// A record the user may not update is reported as missing, not forbidden.
			Name:            "readonly cannot update billing config",
			Method:          http.MethodPatch,
			URL:             "/api/collections/system_billing/records/" + existing.Id,
			Body:            strings.NewReader(`{"price":30}`),
			Headers:         auth(readonlyToken),
			ExpectedStatus:  404,
			ExpectedContent: []string{"wasn't found"},
		},
		{
			// Order matters: the price edit runs first and must keep remindLevel at 7
			// (set below), then moving the expiry re-arms the ladder.
			Name:            "editing other fields keeps the reminder level",
			Method:          http.MethodPatch,
			URL:             "/api/collections/system_billing/records/" + existing.Id,
			Body:            strings.NewReader(`{"price":40}`),
			Headers:         auth(memberToken),
			ExpectedStatus:  200,
			ExpectedContent: []string{`"remindLevel":7`, `"price":40`},
		},
		{
			Name:            "moving the expiry resets the reminder level",
			Method:          http.MethodPatch,
			URL:             "/api/collections/system_billing/records/" + existing.Id,
			Body:            strings.NewReader(`{"expiresAt":"2027-03-14 00:00:00.000Z"}`),
			Headers:         auth(memberToken),
			ExpectedStatus:  200,
			ExpectedContent: []string{`"remindLevel":0`},
		},
		{
			Name:            "nobody can write traffic through the API",
			Method:          http.MethodPost,
			URL:             "/api/collections/system_traffic/records",
			Body:            strings.NewReader(`{"system":"` + memberSystem + `","usedUpload":1}`),
			Headers:         auth(memberToken),
			ExpectedStatus:  403,
			ExpectedContent: []string{"Only superusers can perform this action"},
		},
		{
			Name:            "outsiders cannot see another system's billing",
			Method:          http.MethodGet,
			URL:             "/api/collections/system_billing/records",
			Headers:         auth(outsiderToken),
			ExpectedStatus:  200,
			ExpectedContent: []string{`"totalItems":0`},
		},
		{
			Name:            "readonly members can read billing",
			Method:          http.MethodGet,
			URL:             "/api/collections/system_billing/records",
			Headers:         auth(readonlyToken),
			ExpectedStatus:  200,
			ExpectedContent: []string{`"totalItems":2`, existing.Id},
		},
	}

	factory := func(t testing.TB) *pbtests.TestApp { return hub.TestApp }
	for i := range scenarios {
		scenarios[i].TestAppFactory = factory
		scenarios[i].Test(t)
	}
}
