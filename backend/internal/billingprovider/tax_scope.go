package billingprovider

import (
	"fmt"
	"math"
)

// TaxCountry is a supported tax country code.
type TaxCountry string

// TaxMode describes the tax calculation mode.
type TaxMode string

const (
	TaxModeBGExclusive20 TaxMode = "bg_exclusive_20" // 20% exclusive, domestic BG
)

// TaxScope defines the approved tax configuration for v1.
// Only BG/EUR/20% exclusive is supported. Criteria D: this is not a
// global policy — it is a dev/CI fixture only.
type TaxScope struct {
	Country  TaxCountry
	Currency string
	Mode     TaxMode
}

// ErrTaxScopeUnsupported is returned when the requested tax scope is not
// supported in v1. The provider object is NOT created (criteria A).
type ErrTaxScopeUnsupported struct {
	Country  string
	Currency string
	Mode     string
}

func (e ErrTaxScopeUnsupported) Error() string {
	return fmt.Sprintf("BILLING_TAX_SCOPE_UNSUPPORTED: country=%s currency=%s mode=%s", e.Country, e.Currency, e.Mode)
}

// TaxResolver computes tax for a line item.
// The interface is preserved for future approved configurations (criteria C).
type TaxResolver interface {
	// ComputeTax returns the tax amount in minor units for the given net amount.
	ComputeTax(netMinor int64, scope TaxScope) (int64, error)
}

// BGExclusiveTaxResolver implements the v1 BG 20% exclusive fixture.
type BGExclusiveTaxResolver struct{}

// Validate returns nil if the scope is the approved BG 20% fixture.
// Criteria A: any other scope returns ErrTaxScopeUnsupported before
// provider objects are created.
func (BGExclusiveTaxResolver) Validate(scope TaxScope) error {
	if scope.Country != "BG" || scope.Currency != "EUR" || scope.Mode != TaxModeBGExclusive20 {
		return ErrTaxScopeUnsupported{Country: string(scope.Country), Currency: scope.Currency, Mode: string(scope.Mode)}
	}
	return nil
}

// ComputeTax applies 20% exclusive tax with half-up rounding per line (criteria B).
func (BGExclusiveTaxResolver) ComputeTax(netMinor int64, scope TaxScope) (int64, error) {
	r := BGExclusiveTaxResolver{}
	if err := r.Validate(scope); err != nil {
		return 0, err
	}
	// Half-up rounding: round(netMinor * 0.20).
	tax := float64(netMinor) * 0.20
	return int64(math.Round(tax)), nil
}

// DefaultBGTaxScope returns the approved v1 fixture scope.
func DefaultBGTaxScope() TaxScope {
	return TaxScope{Country: "BG", Currency: "EUR", Mode: TaxModeBGExclusive20}
}
