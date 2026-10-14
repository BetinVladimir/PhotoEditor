package workflow

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"reflect"
	"testing"
)

type catalogPorts struct {
	batch  IngestCatalogBatch
	calls  []string
	failAt string
}

func (p *catalogPorts) Load(context.Context, uuid.UUID, IngestSpec) (IngestCatalogBatch, error) {
	return p.batch, nil
}
func (p *catalogPorts) Prepare(_ context.Context, s OriginalScope) (review.ArtifactRef, error) {
	p.calls = append(p.calls, fmt.Sprint("original/", s.Ordinal))
	return review.ArtifactRef{ID: uuid.New(), SHA256: s.Source.SHA256}, nil
}
func (p *catalogPorts) AdoptImage(_ context.Context, _, _ uuid.UUID, i CatalogImage) (bool, error) {
	return p.step(fmt.Sprint("adopt/", i.Ordinal))
}
func (p *catalogPorts) RejectFile(_ context.Context, _, _ uuid.UUID, f IngestItemFailure) (bool, error) {
	return p.step(fmt.Sprint("reject/", f.Ordinal))
}
func (p *catalogPorts) Finish(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	p.calls = append(p.calls, "finish")
	return "completed_with_errors", nil
}
func (p *catalogPorts) step(name string) (bool, error) {
	p.calls = append(p.calls, name)
	if name == p.failAt {
		return false, errors.New("database unavailable")
	}
	return true, nil
}
func catalogCoordinatorFixture(t *testing.T) (IngestSpec, *catalogPorts) {
	spec, _ := ingestFixture(t)
	item := spec.Items[0]
	img := CatalogImage{Ordinal: item.Ordinal, Source: item.Source, DecodeTaskID: uuid.New(), Metadata: IngestMetadata{Source: item.Source}, Outputs: DecodeOutputBinding{Manifest: review.ArtifactRef{ID: uuid.New(), SHA256: item.Source.SHA256}}}
	other := spec.Items[1]
	p := &catalogPorts{batch: IngestCatalogBatch{Ready: true, Images: []CatalogImage{img}, Failures: []IngestItemFailure{{other.Ordinal, other.Source, "asset.verify", "failed"}}}}
	return spec, p
}
func TestCatalogCoordinatorOrderedSettlementAndFinalBarrier(t *testing.T) {
	spec, p := catalogCoordinatorFixture(t)
	done, err := (IngestCatalogCoordinator{p, p, p}).Advance(context.Background(), uuid.New(), spec)
	if err != nil || !done || !reflect.DeepEqual(p.calls, []string{"reject/2", "original/8", "adopt/8", "finish"}) {
		t.Fatal("wrong catalog order", p.calls, err)
	}
}
func TestCatalogCoordinatorPartialFailureNeverFinishes(t *testing.T) {
	spec, p := catalogCoordinatorFixture(t)
	p.failAt = "adopt/8"
	c := IngestCatalogCoordinator{p, p, p}
	tenant := uuid.New()
	if done, err := c.Advance(context.Background(), tenant, spec); err == nil || done {
		t.Fatal("partial batch finished")
	}
	for _, call := range p.calls {
		if call == "finish" {
			t.Fatal("premature finish")
		}
	}
	p.failAt = ""
	p.calls = nil
	if done, err := c.Advance(context.Background(), tenant, spec); err != nil || !done {
		t.Fatal("resume failed", err)
	}
}
func TestCatalogCoordinatorPendingAndInvalidBatchDoNotMutate(t *testing.T) {
	for _, mode := range []string{"pending", "missing", "duplicate", "wrong source"} {
		t.Run(mode, func(t *testing.T) {
			spec, p := catalogCoordinatorFixture(t)
			switch mode {
			case "pending":
				p.batch.Ready = false
			case "missing":
				p.batch.Failures = nil
			case "duplicate":
				p.batch.Failures[0].Ordinal = p.batch.Images[0].Ordinal
			case "wrong source":
				p.batch.Failures[0].Source.ID = uuid.New()
			}
			done, err := (IngestCatalogCoordinator{p, p, p}).Advance(context.Background(), uuid.New(), spec)
			if done || len(p.calls) != 0 || (mode == "pending" && err != nil) || (mode != "pending" && err == nil) {
				t.Fatal("invalid batch mutated state", err)
			}
		})
	}
}

func TestCatalogCoordinatorAllFailedSkipsOriginals(t *testing.T) {
	spec, p := catalogCoordinatorFixture(t)
	p.batch.Images = nil
	p.batch.Failures = nil
	for _, item := range spec.Items {
		p.batch.Failures = append(p.batch.Failures, IngestItemFailure{item.Ordinal, item.Source, "asset.decode", "failed"})
	}
	done, err := (IngestCatalogCoordinator{p, p, p}).Advance(context.Background(), uuid.New(), spec)
	if err != nil || !done || !reflect.DeepEqual(p.calls, []string{"reject/2", "reject/8", "finish"}) {
		t.Fatal("failed batch attempted original IO", p.calls, err)
	}
}

func TestImportedCatalogUsesExistingOriginalAndOrderedSettlement(t *testing.T) {
 spec, p := catalogCoordinatorFixture(t)
 done, err := (IngestCatalogCoordinator{Source:p, Catalog:p}).advance(context.Background(), uuid.New(), spec, false)
 if err != nil || !done || !reflect.DeepEqual(p.calls, []string{"reject/2", "adopt/8", "finish"}) {
  t.Fatal("imported catalog copied original or changed settlement order", p.calls, err)
 }
}

func TestImportedCatalogValidatesWholeBatchBeforeMutation(t *testing.T) {
 spec, p := catalogCoordinatorFixture(t)
 p.batch.Failures[0].Source.ID = uuid.New()
 done, err := (IngestCatalogCoordinator{Source:p, Catalog:p}).advance(context.Background(), uuid.New(), spec, false)
 if err == nil || done || len(p.calls) != 0 { t.Fatal("invalid imported batch mutated catalog", p.calls, err) }
}

func TestImportedCatalogFailureDoesNotFinish(t *testing.T) {
 spec, p := catalogCoordinatorFixture(t)
 p.failAt = "adopt/8"
 done, err := (IngestCatalogCoordinator{Source:p, Catalog:p}).advance(context.Background(), uuid.New(), spec, false)
 if err == nil || done || !reflect.DeepEqual(p.calls, []string{"reject/2", "adopt/8"}) {
  t.Fatal("imported catalog finished partial settlement", p.calls, err)
 }
}
