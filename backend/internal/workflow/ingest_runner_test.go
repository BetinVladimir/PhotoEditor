package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

type ingestRunnerPorts struct {
	current                   IngestRunContext
	loads, inspection, decode int
	err                       error
}

func (p *ingestRunnerPorts) Load(context.Context, uuid.UUID, uuid.UUID) (IngestRunContext, error) {
	p.loads++
	return p.current, p.err
}
func (p *ingestRunnerPorts) PrepareInspection(context.Context, uuid.UUID, IngestSpec) error {
	p.inspection++
	return p.err
}
func (p *ingestRunnerPorts) Advance(context.Context, uuid.UUID, IngestSpec) (bool, error) {
	p.decode++
	return true, p.err
}

func TestIngestRunnerRoutesOnlyMutablePhases(t *testing.T) {
	for _, phase := range []string{"planning", "inspection", "decode_build", "decode", "catalog", "complete"} {
		t.Run(phase, func(t *testing.T) {
			spec, _ := ingestFixture(t)
			p := &ingestRunnerPorts{current: IngestRunContext{phase, spec}}
			changed, err := (IngestRunner{p, p, p, nil}).Advance(context.Background(), uuid.New(), spec.RunID)
			if phase == "complete" || phase == "decode" || phase == "catalog" {
				if err == nil {
					t.Fatal("unknown phase accepted")
				}
				return
			}
			if err != nil || p.loads != 1 {
				t.Fatal("phase load failed", err)
			}
			if phase == "planning" {
				if !changed || p.inspection != 1 || p.decode != 0 {
					t.Fatal("wrong planning route")
				}
			}
			if phase == "inspection" || phase == "decode_build" {
				if !changed || p.inspection != 0 || p.decode != 1 {
					t.Fatal("wrong decode route")
				}
			}
			if phase == "decode" || phase == "catalog" {
				if changed || p.inspection != 0 || p.decode != 0 {
					t.Fatal("sealed phase rebuilt")
				}
			}
		})
	}
}
func TestIngestRunnerReloadsPhaseAfterUncertainSeal(t *testing.T) {
	spec, _ := ingestFixture(t)
	p := &ingestRunnerPorts{current: IngestRunContext{"planning", spec}}
	runner := IngestRunner{p, p, p, nil}
	tenant := uuid.New()
	if _, err := runner.Advance(context.Background(), tenant, spec.RunID); err != nil {
		t.Fatal(err)
	}
	p.current.Phase = "inspection"
	if _, err := runner.Advance(context.Background(), tenant, spec.RunID); err != nil {
		t.Fatal(err)
	}
	p.current.Phase = "decode"
	if changed, err := runner.Advance(context.Background(), tenant, spec.RunID); err == nil || changed {
		t.Fatal("missing catalog accepted")
	}
	if p.inspection != 1 || p.decode != 1 || p.loads != 3 {
		t.Fatal("cached phase or repeated publication")
	}
	p.err = errors.New("unavailable")
	if _, err := runner.Advance(context.Background(), tenant, spec.RunID); err == nil || p.inspection != 1 || p.decode != 1 {
		t.Fatal("failed load caused mutation")
	}
}

func TestIngestRunnerRoutesDecodeToCatalogWithoutRebuilding(t *testing.T) {
	for _, phase := range []string{"decode", "catalog"} {
		t.Run(phase, func(t *testing.T) {
			spec, _ := ingestFixture(t)
			p := &ingestRunnerPorts{current: IngestRunContext{phase, spec}}
			catalog := &ingestRunnerPorts{}
			changed, err := (IngestRunner{p, p, p, catalog}).Advance(context.Background(), uuid.New(), spec.RunID)
			if err != nil || !changed || p.loads != 1 || p.inspection != 0 || p.decode != 0 || catalog.decode != 1 {
				t.Fatal("catalog route rebuilt graph", err)
			}
		})
	}
}
