package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func TestReflectionCoordinatorSeparatesCatalogAndScheduler(t *testing.T) {
	scheduler, catalog := &pgxpool.Pool{}, &pgxpool.Pool{}
	objects := &objectstore.Store{}
	project, base := uuid.New(), uuid.New()
	got, err := NewSQLReflectionRestorationCoordinator(scheduler, catalog,
		objects, "photos", project, base)
	if err != nil || got.CatalogProject != project || got.BaseAssetVersion != base ||
		got.DB != scheduler || got.Objects != objects ||
		got.ParamsPublisher == nil || got.Transport == nil {
		t.Fatal("reflection coordinator lost installed dependencies", err)
	}
	if _, err := NewSQLReflectionRestorationCoordinator(scheduler, scheduler,
		objects, "photos", project, base); err != v2input.ErrInvalid {
		t.Fatal("shared scheduler/editor pool accepted", err)
	}
	submitter, err := NewSQLReflectionRestorationSubmitter(scheduler, catalog,
		objects, "photos", project, base)
	if err != nil || submitter.Beginner == nil || submitter.Coordinator.DB != scheduler ||
		submitter.Coordinator.CatalogProject != project {
		t.Fatal("reflection submitter lost scoped coordinator", err)
	}
}

func TestReflectionCoordinatorRefusesMismatchedProposalBeforeIO(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	pin := v2portrait.RestorationQualificationPin{Operation: "image.reflection",
		OperationVersion: "1", PolicyID: uuid.New(), QualificationID: uuid.New(),
		PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha,
		RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
		IssuerID: uuid.New(), Backend: "cpu"}
	project, base := uuid.New(), uuid.New()
	coordinator := ReflectionRestorationCoordinator{CatalogProject: project,
		BaseAssetVersion: base, DB: restorationStoreDB{&restorationStoreTx{}},
		ParamsPublisher: &recoveryAdmissionPublisher{},
		Transport:       &recoveryAdmissionPublisher{},
		BaseCatalog:     &SQLArtifactCatalog{}, Objects: &objectstore.Store{}}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	foreign, err := canonical.Marshal(map[string]any{
		"base":           map[string]any{"asset_version_id": uuid.New()},
		"model_lock_sha": sha,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = coordinator.Admit(context.Background(), who, uuid.New(), project,
		uuid.New(), uuid.New(), uuid.New(), 0,
		foreign,
		[]byte(`{}`), pin)
	if err != v2input.ErrInvalid {
		t.Fatal("foreign base version reached object or SQL reads", err)
	}
}
