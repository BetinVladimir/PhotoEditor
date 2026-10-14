package v2draft

import (
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"testing"
)

type recoveryArtifactCatalog struct{ calls *int }

func (c recoveryArtifactCatalog) Lookup(context.Context, auth.Identity, uuid.UUID, LayerRef) (objectstore.Object, error) {
	*c.calls++
	return objectstore.Object{}, v2input.ErrInvalid
}

type recoveryObjectReader struct{ calls *int }

func (r recoveryObjectReader) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	*r.calls++
	return nil, v2input.ErrInvalid
}
func TestDonorRecoveryPreflightNeverReadsUnboundEvidence(t *testing.T) {
	for _, mode := range []string{"zero_seed", "cancelled", "zero_patch", "missing_catalog"} {
		t.Run(mode, func(t *testing.T) {
			catalogCalls, reads, qualCalls := 0, 0, 0
			c := recoveryArtifactCatalog{&catalogCalls}
			r := recoveryObjectReader{&reads}
			q := recoveryQualificationCatalogFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2portrait.DonorQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
				qualCalls++
				return "", objectstore.Object{}, objectstore.Object{}, v2input.ErrInvalid
			})
			loader := DonorRecoveryLoader{Source: ClosureResolver{Catalog: c, Objects: r}, DonorCatalog: c, QualificationCatalog: q, PatchWidth: 32, PatchHeight: 32}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "zero_patch" {
				loader.PatchWidth = 0
			}
			if mode == "missing_catalog" {
				loader.DonorCatalog = nil
			}
			plan, evidence, err := loader.Reload(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), uuid.New(), DonorParamsRecoverySeed{}, DonorManualProducer{})
			if err == nil || catalogCalls != 0 || reads != 0 || qualCalls != 0 || plan.Plan.Params != nil || evidence.Entries != nil {
				t.Fatal("invalid recovery leaked evidence/IO")
			}
		})
	}
}
