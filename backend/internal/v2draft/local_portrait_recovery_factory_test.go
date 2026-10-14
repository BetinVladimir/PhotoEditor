package v2draft

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestLocalPortraitRecoveryFactoryScopes(t *testing.T) {
	db := &pgxpool.Pool{}
	catalogDB := &pgxpool.Pool{}
	factory := SQLLocalPortraitRecoveryFactory{DB: db, CatalogDB: catalogDB, Bucket: "photos", Objects: closureFixture{}, Documents: localTaskPublisherFunc(func(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error) {
		t.Fatal("factory performed IO")
		return artifactjson.Ref{}, nil
	})}
	first := LocalPortraitOwnerRequest{SchemaVersion: 1, Tenant: uuid.New(), Project: uuid.New(), Intent: LocalPortraitIntent{Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New()}}}
	second := first
	second.Project = uuid.New()
	second.Intent.Binding.AssetVersionID = uuid.New()
	var catalogs []SQLArtifactCatalog
	for _, request := range []LocalPortraitOwnerRequest{first, second} {
		material, coordinator, err := factory.Build(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		catalog, ok := coordinator.Resolver.Catalog.(SQLArtifactCatalog)
		if !ok || catalog.ProjectID != request.Project || catalog.AssetVersionID != request.Intent.Binding.AssetVersionID || catalog.DB != catalogDB || catalog.Bucket != "photos" {
			t.Fatal("wrong saved scope")
		}
		typed, ok := material.(SQLLocalPortraitMaterialLoader)
		if !ok || typed.DB != db || coordinator.Owners.(SQLLocalPortraitOwnerScope).DB != db || typed.Resolver.Catalog.(SQLArtifactCatalog) != catalog {
			t.Fatal("material and admission scopes differ")
		}
		catalogs = append(catalogs, catalog)
	}
	if catalogs[0].ProjectID != first.Project || catalogs[0].AssetVersionID != first.Intent.Binding.AssetVersionID {
		t.Fatal("second build mutated first scope")
	}
	missingCatalog := factory
	missingCatalog.CatalogDB = nil
	if material, coordinator, err := missingCatalog.Build(context.Background(), first); err == nil || material != nil || coordinator.Owners != nil {
		t.Fatal("missing API catalog pool accepted")
	}
	invalid := first
	invalid.Project = uuid.Nil
	if material, coordinator, err := factory.Build(context.Background(), invalid); err == nil || material != nil || coordinator.Owners != nil {
		t.Fatal("invalid scope built")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := factory.Build(ctx, first); err != context.Canceled {
		t.Fatal("cancellation ignored")
	}
}
