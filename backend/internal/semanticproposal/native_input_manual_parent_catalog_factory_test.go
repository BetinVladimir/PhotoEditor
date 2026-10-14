package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
)

// Factory tests validate scope routing only, not physical artifact authority.
func TestNativeParentCatalogFactoryUsesOriginalScopeAndVersion(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	project := uuid.New()
	anchor := nativePreparationAnchor{Request: Request{TenantID: uuid.New(), ActorID: who.UserID, RunID: uuid.New()}, Published: PublishedIntent{Inventory: CatalogInventory{Scope: CatalogScope{ProjectID: project}}}}
	anchor.Request.Intent.Reference.AssetVersionID = uuid.New()
	called := 0
	factory := func(ctx context.Context, got auth.Identity, tenant, p, v uuid.UUID) (v2draft.ArtifactCatalog, error) {
		called++
		if got != who || tenant != anchor.Request.TenantID || p != project || v != anchor.Request.Intent.Reference.AssetVersionID {
			t.Fatal("catalog factory used another scope")
		}
		return v2draft.SQLArtifactCatalog{ProjectID: p, AssetVersionID: v}, nil
	}
	catalog, err := resolveNativeParentRecoveryCatalog(context.Background(), who, anchor, project, nil, factory)
	if err != nil || called != 1 {
		t.Fatal("original scope refused", err)
	}
	scoped := catalog.(v2draft.SQLArtifactCatalog)
	if scoped.ProjectID != project || scoped.AssetVersionID != anchor.Request.Intent.Reference.AssetVersionID {
		t.Fatal("original version not retained")
	}
	changed := anchor
	changed.Request.ActorID = uuid.New()
	if catalog, err := resolveNativeParentRecoveryCatalog(context.Background(), who, changed, project, nil, factory); err == nil || catalog != nil || called != 1 {
		t.Fatal("wrong actor reached factory")
	}
	changed = anchor
	changed.Published.Inventory.Scope.ProjectID = uuid.New()
	if catalog, err := resolveNativeParentRecoveryCatalog(context.Background(), who, changed, project, nil, factory); err == nil || catalog != nil || called != 1 {
		t.Fatal("wrong project reached factory")
	}
}

func TestNativeParentCatalogFactoryCancellationAndErrorsExposeNoCatalog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if catalog, err := (SQLNativeParentArtifactCatalogFactory{}).ForReference(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New()); err != context.Canceled || catalog != nil {
		t.Fatal("cancelled SQL factory reached database", err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	project := uuid.New()
	anchor := nativePreparationAnchor{Request: Request{TenantID: uuid.New(), ActorID: who.UserID, RunID: uuid.New()}, Published: PublishedIntent{Inventory: CatalogInventory{Scope: CatalogScope{ProjectID: project}}}}
	anchor.Request.Intent.Reference.AssetVersionID = uuid.New()
	failure := errors.New("source catalog unavailable")
	for _, mode := range []string{"error", "nil", "cancelled"} {
		ctx, stop := context.WithCancel(context.Background())
		factory := func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) (v2draft.ArtifactCatalog, error) {
			switch mode {
			case "error":
				return nil, failure
			case "cancelled":
				stop()
				return v2draft.SQLArtifactCatalog{}, nil
			}
			return nil, nil
		}
		catalog, err := resolveNativeParentRecoveryCatalog(ctx, who, anchor, project, nil, factory)
		stop()
		if err == nil || catalog != nil {
			t.Fatal("failed factory exposed catalog", mode)
		}
		if mode == "error" && !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if mode == "cancelled" && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
