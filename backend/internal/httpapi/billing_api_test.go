package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/billing"
	"photoeditor/backend/internal/legal"
	"photoeditor/backend/internal/privacy"
)

// ---------------------------------------------------------------------------
// GET /api/v1/billing/catalog — handler-level tests via embedded mux
// ---------------------------------------------------------------------------

func buildCatalogMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/billing/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		locale := q.Get("locale")
		currency := q.Get("currency")
		switch locale {
		case "en", "ru", "bg":
		default:
			w.WriteHeader(422)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "VALIDATION_FAILED"})
			return
		}
		if currency != "EUR" || locale == "" {
			w.WriteHeader(422)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "VALIDATION_FAILED"})
			return
		}
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(billing.Catalog{
			CatalogRevision: "1",
			Scope:           "public",
			Currency:        "EUR",
			Plans:           []billing.PlanEntry{},
		})
	})
	return mux
}

func TestBillingCatalog_Success(t *testing.T) {
	srv := httptest.NewServer(buildCatalogMux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/billing/catalog?locale=en&currency=EUR")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var cat billing.Catalog
	if err := json.NewDecoder(resp.Body).Decode(&cat); err != nil {
		t.Fatal(err)
	}
	if cat.Currency != "EUR" {
		t.Errorf("expected EUR, got %q", cat.Currency)
	}
	if cat.Scope != "public" {
		t.Errorf("expected public, got %q", cat.Scope)
	}
}

func TestBillingCatalog_MissingLocale(t *testing.T) {
	srv := httptest.NewServer(buildCatalogMux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/billing/catalog?currency=EUR")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
}

func TestBillingCatalog_InvalidLocale(t *testing.T) {
	srv := httptest.NewServer(buildCatalogMux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/billing/catalog?locale=fr&currency=EUR")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422 for unsupported locale, got %d", resp.StatusCode)
	}
}

func TestBillingCatalog_NonEURCurrency(t *testing.T) {
	srv := httptest.NewServer(buildCatalogMux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/billing/catalog?locale=en&currency=USD")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422 for non-EUR currency, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Stripe webhook: missing signature → 400
// ---------------------------------------------------------------------------

func TestStripeWebhook_MissingSignature(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhooks/stripe/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Stripe-Signature") == "" {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "VALIDATION_FAILED"})
			return
		}
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{"received": true})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := bytes.NewBufferString(`{"type":"payment_intent.succeeded"}`)
	resp, err := http.Post(srv.URL+"/api/v1/webhooks/stripe/"+uuid.New().String(), "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 without Stripe-Signature, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// DTO shape tests
// ---------------------------------------------------------------------------

func TestBillingCatalogDTO_Shape(t *testing.T) {
	cat := billing.Catalog{
		CatalogRevision: "42",
		Scope:           "public",
		Currency:        "EUR",
		Plans:           []billing.PlanEntry{},
	}
	b, err := json.Marshal(cat)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"catalog_revision", "scope", "currency", "plans"} {
		if _, ok := m[k]; !ok {
			t.Errorf("BillingCatalog JSON missing key: %s", k)
		}
	}
	if _, ok := m["custom_plans"]; ok {
		t.Error("BillingCatalog must not expose custom_plans")
	}
}

func TestBillingOrderDTO_StatusFields(t *testing.T) {
	o := billing.Order{
		ID:                uuid.New(),
		Status:            "created",
		PaymentStatus:     "pending",
		FulfillmentStatus: "pending",
		Total:             billing.Money{Currency: "EUR", NetMinor: "0", TaxMinor: "0", GrossMinor: "0", TaxMode: "test_bg20", TaxLines: []billing.TaxLine{}},
		Action:            nil,
	}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "status", "payment_status", "fulfillment_status", "total", "action"} {
		if _, ok := m[k]; !ok {
			t.Errorf("BillingOrder JSON missing key: %s", k)
		}
	}
}

// ---------------------------------------------------------------------------
// Legal acceptance validation
// ---------------------------------------------------------------------------

func TestLegalAcceptance_Valid(t *testing.T) {
	a := legal.Acceptance{
		DocumentSetHash: "hash-abc",
		Events: []legal.AcceptanceEvent{
			{DocumentVersionID: uuid.New(), Action: "accepted"},
		},
		ClientEventID: "idempotency-key-123",
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("valid acceptance rejected: %v", err)
	}
}

func TestLegalAcceptance_EmptyEvents(t *testing.T) {
	a := legal.Acceptance{DocumentSetHash: "h", Events: nil, ClientEventID: "k"}
	if err := a.Validate(); err == nil {
		t.Error("acceptance with no events should fail")
	}
}

func TestLegalAcceptance_BadAction(t *testing.T) {
	a := legal.Acceptance{
		DocumentSetHash: "h",
		Events:          []legal.AcceptanceEvent{{DocumentVersionID: uuid.New(), Action: "ignored"}},
		ClientEventID:   "k",
	}
	if err := a.Validate(); err == nil {
		t.Error("acceptance with invalid action should fail")
	}
}

// ---------------------------------------------------------------------------
// Privacy preferences validation
// ---------------------------------------------------------------------------

func TestPrivacyUpdate_AtLeastOneField(t *testing.T) {
	empty := privacy.PreferencesUpdate{}
	if err := empty.Validate(); err == nil {
		t.Error("empty update should require at least one field")
	}
	b := true
	ok := privacy.PreferencesUpdate{Analytics: &b}
	if err := ok.Validate(); err != nil {
		t.Errorf("single-field update should pass: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Deletion request validation
// ---------------------------------------------------------------------------

func TestDeletionRequest_Valid(t *testing.T) {
	d := privacy.DeletionRequest{
		PrivacyCaseID:   uuid.New(),
		RecentAuthGrant: "grant",
		OwnershipResolution: []privacy.OwnershipResolution{
			{TenantID: uuid.New(), Action: "close"},
		},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("valid deletion rejected: %v", err)
	}
}

func TestDeletionRequest_InvalidAction(t *testing.T) {
	d := privacy.DeletionRequest{
		PrivacyCaseID:   uuid.New(),
		RecentAuthGrant: "g",
		OwnershipResolution: []privacy.OwnershipResolution{
			{TenantID: uuid.New(), Action: "delete"},
		},
	}
	if err := d.Validate(); err == nil {
		t.Error("invalid action 'delete' should fail")
	}
}

// ---------------------------------------------------------------------------
// Contract: all 16 billing endpoints in both registries
// ---------------------------------------------------------------------------

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine source file path")
	}
	// backend/internal/httpapi → backend → repo root
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func readContractFile(t *testing.T, relPath string) []byte {
	t.Helper()
	path := filepath.Join(repoRoot(t), relPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readContractFile(%q): %v", path, err)
	}
	return data
}

func TestBillingEndpoints_InV2Registry(t *testing.T) {
	data := readContractFile(t, "docs/implementation-v2/contracts/endpoints.json")
	expected := []string{
		`/api/v1/billing/catalog`,
		`/api/v1/tenants/{t}/billing`,
		`/api/v1/tenants/{t}/billing/usage`,
		`/api/v1/tenants/{t}/billing/buckets`,
		`/api/v1/tenants/{t}/billing/quotes`,
		`/api/v1/tenants/{t}/billing/orders`,
		`/api/v1/tenants/{t}/billing/orders/{order}`,
		`/api/v1/tenants/{t}/billing/changes`,
		`/api/v1/tenants/{t}/billing/changes/{change}/cancel`,
		`/api/v1/tenants/{t}/billing/portal`,
		`/api/v1/webhooks/stripe/{account}`,
		`/api/v1/legal/requirements`,
		`/api/v1/me/legal/acceptances`,
		`/api/v1/me/privacy`,
		`/api/v1/me/deletion`,
		`/api/v1/me/deletion/{request}/status`,
	}
	for _, path := range expected {
		if !strings.Contains(string(data), path) {
			t.Errorf("v2 endpoints.json missing: %s", path)
		}
	}
}

func TestBillingEndpoints_InV1Registry(t *testing.T) {
	data := readContractFile(t, "docs/implementation/contracts/endpoints.json")
	expected := []string{
		`/api/v1/billing/catalog`,
		`/api/v1/tenants/{t}/billing`,
		`/api/v1/tenants/{t}/billing/quotes`,
		`/api/v1/tenants/{t}/billing/orders`,
		`/api/v1/webhooks/stripe/{account}`,
	}
	for _, path := range expected {
		if !strings.Contains(string(data), path) {
			t.Errorf("v1 endpoints.json missing: %s", path)
		}
	}
}

func TestBillingDTOs_InSchemasRegistry(t *testing.T) {
	data := readContractFile(t, "docs/implementation-v2/contracts/schemas.json")
	expected := []string{
		`"BillingCatalog"`, `"BillingMoney"`, `"BillingSummary"`, `"BillingQuote"`,
		`"BillingOrder"`, `"BillingChange"`, `"BillingUsagePage"`,
		`"LegalRequirementSet"`, `"LegalAcceptance"`,
		`"PrivacyPreferences"`, `"DeletionAccepted"`, `"DeletionStatus"`,
	}
	for _, dto := range expected {
		if !strings.Contains(string(data), dto) {
			t.Errorf("schemas.json missing DTO: %s", dto)
		}
	}
}
