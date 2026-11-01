package billingprovider_test

import (
	"context"
	"testing"

	"photoeditor/backend/internal/billingprovider"
)

func makeTestCatalog() ([]billingprovider.CatalogItem, []billingprovider.PriceItem) {
	items := []billingprovider.CatalogItem{
		{MappingKey: "plan:free:v1", Name: "Free", Description: "Free plan"},
		{MappingKey: "plan:pro:v1", Name: "Pro", Description: "Pro plan"},
	}
	prices := []billingprovider.PriceItem{
		// trial: no provider price (criteria C)
		{MappingKey: "price:free:trial", ProductKey: "plan:free:v1", IsTrial: true},
		// paid prices
		{MappingKey: "price:pro:monthly:v1", ProductKey: "plan:pro:v1", AmountMinor: 999, Currency: "eur", Interval: "month"},
		{MappingKey: "price:pro:yearly:v1", ProductKey: "plan:pro:v1", AmountMinor: 9990, Currency: "eur", Interval: "year"},
	}
	return items, prices
}

func newProvisioner() (*billingprovider.Provisioner, *billingprovider.FakeProvider, *billingprovider.InMemoryMappingStore) {
	p := billingprovider.NewFakeProvider()
	s := billingprovider.NewInMemoryMappingStore()
	return billingprovider.NewProvisioner(p, s), p, s
}

// Criteria A: repeat run does not create new Products or Prices.
func TestProvisioner_IdempotentRepeat(t *testing.T) {
	ctx := context.Background()
	prov, _, _ := newProvisioner()
	items, prices := makeTestCatalog()

	r1, err := prov.Run(ctx, "v1", "acct_test", items, prices)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	r2, err := prov.Run(ctx, "v1", "acct_test", items, prices)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	// Product IDs must be identical on both runs
	if len(r1.Products) != len(r2.Products) {
		t.Fatalf("product count mismatch: %d vs %d", len(r1.Products), len(r2.Products))
	}
	for i, p1 := range r1.Products {
		p2 := r2.Products[i]
		if p1.ProviderID != p2.ProviderID {
			t.Errorf("product[%d] %q: ID changed on repeat: %q → %q", i, p1.MappingKey, p1.ProviderID, p2.ProviderID)
		}
	}

	// Price IDs must be identical (for non-trial)
	if len(r1.Prices) != len(r2.Prices) {
		t.Fatalf("price count mismatch: %d vs %d", len(r1.Prices), len(r2.Prices))
	}
	for i, pp1 := range r1.Prices {
		pp2 := r2.Prices[i]
		if !pp1.IsTrial && pp1.ProviderID != pp2.ProviderID {
			t.Errorf("price[%d] %q: ID changed on repeat: %q → %q", i, pp1.MappingKey, pp1.ProviderID, pp2.ProviderID)
		}
	}
}

// Criteria B: changed price creates a new Stripe Price and new local version.
func TestProvisioner_ChangedPrice_NewVersion(t *testing.T) {
	ctx := context.Background()
	prov, _, store := newProvisioner()
	items := []billingprovider.CatalogItem{
		{MappingKey: "plan:pro:v1", Name: "Pro", Description: "Pro plan"},
	}
	prices := []billingprovider.PriceItem{
		{MappingKey: "price:pro:monthly", ProductKey: "plan:pro:v1", AmountMinor: 999, Currency: "eur", Interval: "month"},
	}

	r1, err := prov.Run(ctx, "v1", "acct_test", items, prices)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstPriceID := r1.Prices[0].ProviderID
	firstVersion := r1.Prices[0].Version

	// Change amount to simulate a price update
	prices[0].AmountMinor = 1299
	r2, err := prov.Run(ctx, "v2", "acct_test", items, prices)
	if err != nil {
		t.Fatalf("second run with new price: %v", err)
	}
	secondPriceID := r2.Prices[0].ProviderID
	secondVersion := r2.Prices[0].Version

	if firstPriceID == secondPriceID {
		t.Error("changed price should create a new provider Price, not reuse the old one")
	}
	if secondVersion <= firstVersion {
		t.Errorf("new price should have higher version: %d → %d", firstVersion, secondVersion)
	}

	// The mapping store should reflect the new version
	m, found, err := store.GetPriceMapping(ctx, "price:pro:monthly")
	if err != nil || !found {
		t.Fatalf("price mapping not found after update: %v", err)
	}
	if m.AmountMinor != 1299 {
		t.Errorf("mapping amount not updated: %d", m.AmountMinor)
	}
}

// Criteria C: trial price produces no provider price.
func TestProvisioner_Trial_NoProviderPrice(t *testing.T) {
	ctx := context.Background()
	prov, _, _ := newProvisioner()
	items := []billingprovider.CatalogItem{
		{MappingKey: "plan:free:v1", Name: "Free", Description: "Free plan"},
	}
	prices := []billingprovider.PriceItem{
		{MappingKey: "price:free:trial", ProductKey: "plan:free:v1", IsTrial: true},
	}

	r, err := prov.Run(ctx, "v1", "acct_test", items, prices)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(r.Prices) != 1 {
		t.Fatalf("expected 1 price result, got %d", len(r.Prices))
	}
	trial := r.Prices[0]
	if !trial.IsTrial {
		t.Error("trial price should have IsTrial=true")
	}
	if trial.ProviderID != "" {
		t.Errorf("trial price must have empty ProviderID, got %q", trial.ProviderID)
	}
}

// Criteria D: local amount/interval/currency match the provider price.
func TestProvisioner_LocalMatchesProvider(t *testing.T) {
	ctx := context.Background()
	prov, fp, _ := newProvisioner()
	items := []billingprovider.CatalogItem{
		{MappingKey: "plan:pro:v1", Name: "Pro", Description: "Pro"},
	}
	prices := []billingprovider.PriceItem{
		{MappingKey: "price:pro:monthly", ProductKey: "plan:pro:v1", AmountMinor: 999, Currency: "eur", Interval: "month"},
	}

	r, err := prov.Run(ctx, "v1", "acct_test", items, prices)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	pp := r.Prices[0]
	if pp.AmountMinor != 999 {
		t.Errorf("local amount %d != 999", pp.AmountMinor)
	}
	if pp.Currency != "eur" {
		t.Errorf("local currency %q != eur", pp.Currency)
	}
	if pp.Interval != "month" {
		t.Errorf("local interval %q != month", pp.Interval)
	}

	// Verify provider also holds the same values
	provPrice, err := fp.GetPrice(ctx, pp.ProviderID)
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if provPrice.AmountMinor != 999 {
		t.Errorf("provider amount %d != 999", provPrice.AmountMinor)
	}
	if provPrice.Currency != "eur" {
		t.Errorf("provider currency %q != eur", provPrice.Currency)
	}
	if provPrice.Interval != "month" {
		t.Errorf("provider interval %q != month", provPrice.Interval)
	}
}

// Verify provisioning works for the full catalog (products + prices, multi-item).
func TestProvisioner_FullCatalog(t *testing.T) {
	ctx := context.Background()
	prov, _, _ := newProvisioner()
	items, prices := makeTestCatalog()
	r, err := prov.Run(ctx, "v1", "acct_test", items, prices)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(r.Products) != len(items) {
		t.Errorf("expected %d products, got %d", len(items), len(r.Products))
	}
	if len(r.Prices) != len(prices) {
		t.Errorf("expected %d prices, got %d", len(prices), len(r.Prices))
	}
	for _, pp := range r.Products {
		if pp.ProviderID == "" {
			t.Errorf("product %q has empty ProviderID", pp.MappingKey)
		}
	}
	for _, pp := range r.Prices {
		if !pp.IsTrial && pp.ProviderID == "" {
			t.Errorf("non-trial price %q has empty ProviderID", pp.MappingKey)
		}
	}
}
