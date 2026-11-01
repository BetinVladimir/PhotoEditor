package billingprovider_test

import (
	"errors"
	"testing"

	"photoeditor/backend/internal/billingprovider"
)

// Criteria A: unsupported country/mode/currency → ErrTaxScopeUnsupported.
func TestTaxScope_UnsupportedCountry_Rejected(t *testing.T) {
	r := billingprovider.BGExclusiveTaxResolver{}
	cases := []billingprovider.TaxScope{
		{Country: "US", Currency: "EUR", Mode: billingprovider.TaxModeBGExclusive20},
		{Country: "BG", Currency: "USD", Mode: billingprovider.TaxModeBGExclusive20},
		{Country: "BG", Currency: "EUR", Mode: "custom_mode"},
		{Country: "DE", Currency: "EUR", Mode: billingprovider.TaxModeBGExclusive20},
	}
	for _, scope := range cases {
		_, err := r.ComputeTax(1000, scope)
		var ue billingprovider.ErrTaxScopeUnsupported
		if !errors.As(err, &ue) {
			t.Errorf("scope {%s %s %s} should return ErrTaxScopeUnsupported, got %v",
				scope.Country, scope.Currency, scope.Mode, err)
		}
	}
}

// Criteria A: approved BG scope passes.
func TestTaxScope_BG_EUR_20_Approved(t *testing.T) {
	r := billingprovider.BGExclusiveTaxResolver{}
	scope := billingprovider.DefaultBGTaxScope()
	if err := r.Validate(scope); err != nil {
		t.Errorf("approved scope rejected: %v", err)
	}
}

// Criteria B: 20% with half-up rounding per line.
func TestTaxScope_Rounding_HalfUp(t *testing.T) {
	r := billingprovider.BGExclusiveTaxResolver{}
	scope := billingprovider.DefaultBGTaxScope()

	cases := []struct {
		net      int64
		expected int64
	}{
		{net: 1000, expected: 200},  // 1000 * 0.20 = 200.00 → 200
		{net: 999, expected: 200},   // 999 * 0.20 = 199.80 → 200 (half-up)
		{net: 995, expected: 199},   // 995 * 0.20 = 199.00 → 199
		{net: 997, expected: 199},   // 997 * 0.20 = 199.40 → 199
		{net: 998, expected: 200},   // 998 * 0.20 = 199.60 → 200 (half-up)
		{net: 100, expected: 20},
		{net: 101, expected: 20},    // 101 * 0.20 = 20.20 → 20
		{net: 103, expected: 21},    // 103 * 0.20 = 20.60 → 21
	}
	for _, c := range cases {
		tax, err := r.ComputeTax(c.net, scope)
		if err != nil {
			t.Fatalf("ComputeTax(%d): %v", c.net, err)
		}
		if tax != c.expected {
			t.Errorf("ComputeTax(%d) = %d, want %d", c.net, tax, c.expected)
		}
	}
}

// Criteria C: TaxResolver interface is preserved (compile-time check).
func TestTaxScope_ResolverInterface(t *testing.T) {
	var _ billingprovider.TaxResolver = billingprovider.BGExclusiveTaxResolver{}
}

// Criteria D: default scope is BG only — not applied globally.
func TestTaxScope_DefaultScope_IsBGOnly(t *testing.T) {
	scope := billingprovider.DefaultBGTaxScope()
	if scope.Country != "BG" {
		t.Errorf("default scope must be BG, got %q (criteria D: not global)", scope.Country)
	}
	if scope.Currency != "EUR" {
		t.Errorf("default scope must be EUR, got %q", scope.Currency)
	}
}
