package v2region

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
)

func TestRegionSQLQualificationUnconfiguredAndCancelled(t *testing.T) {
	p, _, _, _ := regionNodeFixture(t)
	s := SQLRegionQualificationResolver{}
	if _, err := s.ResolveRegionQualification(context.Background(), p); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ResolveRegionQualification(ctx, p); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSQLRegionProfileFactoryRequiresExactBundleEvidence(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	p := regionCalibrationFixture(t, plan)
	raw, pin := regionCalibrationBytes(t, []RegionCalibration{p})
	catalog, err := NewPinnedRegionCalibrationCatalog(raw, pin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewSQLRegionProfileProvider(nil, catalog, p.EvidenceSHA); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("missing API accepted", err)
	}
}

func TestSQLRegionProfileFactoryPinsActualCatalogWithoutIO(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	p := regionCalibrationFixture(t, plan)
	raw, pin := regionCalibrationBytes(t, []RegionCalibration{p})
	catalog, err := NewPinnedRegionCalibrationCatalog(raw, pin)
	if err != nil {
		t.Fatal(err)
	}
	// Constructor must not touch this inert pool; live checks occur on resolution.
	api := new(pgxpool.Pool)
	if _, err = NewSQLRegionProfileProvider(api, catalog, strings.Repeat("0", 64)); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("unbound evidence accepted", err)
	}
	provider, err := NewSQLRegionProfileProvider(api, catalog, p.EvidenceSHA)
	if err != nil {
		t.Fatal(err)
	}
	costs := provider.Qualifier.(CalibratedRegionCosts)
	resolver := costs.Resolver.(SQLRegionQualificationResolver)
	if resolver.CalibrationSHA != pin || resolver.EvidenceSHA != p.EvidenceSHA || costs.Catalog != catalog {
		t.Fatal("constructor lost catalog/evidence identity")
	}
}
