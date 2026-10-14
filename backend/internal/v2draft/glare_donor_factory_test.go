package v2draft

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func TestSQLGlareDonorFactorySeparatesCatalogAndSchedulerRoles(t *testing.T) {
	scheduler, catalog := &pgxpool.Pool{}, &pgxpool.Pool{}
	objects := &objectstore.Store{}
	project, base, donor := uuid.New(), uuid.New(), uuid.New()
	coordinator, err := NewSQLGlareDonorRestorationCoordinator(scheduler,
		catalog, objects, "photos", project, base, donor)
	if err != nil {
		t.Fatal(err)
	}
	if coordinator.CatalogProject != project || coordinator.BaseAssetVersion != base ||
		coordinator.DonorAssetVersion != donor {
		t.Fatal("coordinator lost catalog binding")
	}
	if coordinator.DB != scheduler || coordinator.Objects != objects {
		t.Fatal("scheduler or object reader miswired")
	}
	baseCatalog, ok := coordinator.BaseCatalog.(SQLArtifactCatalog)
	if !ok || baseCatalog.DB != catalog || baseCatalog.ProjectID != project ||
		baseCatalog.AssetVersionID != base || baseCatalog.Bucket != "photos" {
		t.Fatal("base catalog lost editor role or version")
	}
	donorCatalog, ok := coordinator.DonorCatalog.(SQLArtifactCatalog)
	if !ok || donorCatalog.DB != catalog || donorCatalog.ProjectID != project ||
		donorCatalog.AssetVersionID != donor {
		t.Fatal("donor catalog lost editor role or version")
	}
	confirmation, ok := coordinator.ConfirmationPublisher.(v2portrait.DonorConfirmationObjectPublisher)
	if !ok || confirmation.Repository.(v2portrait.SQLDonorConfirmationRepository).DB != scheduler ||
		confirmation.Objects != objects {
		t.Fatal("confirmation publisher miswired")
	}
	for _, publisher := range []v2input.DocumentPublisher{coordinator.ParamsPublisher, coordinator.TransportPublisher} {
		verified, ok := publisher.(artifactjson.Publisher)
		if !ok || verified.Repository.(artifactjson.SQLRepository).DB != scheduler ||
			verified.Objects != objects {
			t.Fatal("document publisher miswired")
		}
	}
	if _, err := NewSQLGlareDonorRestorationCoordinator(scheduler, catalog,
		objects, "photos", project, base, base); err != v2input.ErrInvalid {
		t.Fatal("same source and donor version was accepted", err)
	}
	if _, err := NewSQLGlareDonorRestorationCoordinator(scheduler, scheduler,
		objects, "photos", project, base, donor); err != v2input.ErrInvalid {
		t.Fatal("shared scheduler/editor pool was accepted", err)
	}
}
