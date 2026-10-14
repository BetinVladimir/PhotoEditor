package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type glareCountDocuments struct{ calls *int }

func (p glareCountDocuments) Publish(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error) {
	(*p.calls)++
	return artifactjson.Ref{}, v2input.ErrInvalid
}

func TestGlareCoordinatorRejectsCatalogVersionMismatchBeforeSQL(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	pin := v2portrait.RestorationQualificationPin{Operation: "portrait.glare",
		OperationVersion: "1", PolicyID: uuid.New(), QualificationID: uuid.New(),
		PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha,
		RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
		IssuerID: uuid.New(), Backend: "cpu"}
	project, base, donor := uuid.New(), uuid.New(), uuid.New()
	proposal, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "model_lock_sha": pin.ModelLockSHA,
		"calibration": pin.Calibration,
		"base":        map[string]any{"asset_version_id": base},
		"donor":       map[string]any{"asset_version_id": uuid.New()},
	})
	if err != nil {
		t.Fatal(err)
	}
	tx := &restorationStoreTx{role: "photo_scheduler"}
	calls := 0
	cat := &restorationCatalog{objects: map[uuid.UUID]objectstore.Object{}, calls: map[uuid.UUID]int{}}
	coordinator := GlareDonorRestorationCoordinator{CatalogProject: project,
		BaseAssetVersion: base, DonorAssetVersion: donor,
		DB: restorationStoreDB{tx}, ConfirmationPublisher: glareCountConfirmation{&calls},
		ParamsPublisher:    glareCountDocuments{&calls},
		TransportPublisher: glareCountDocuments{&calls},
		BaseCatalog:        cat, DonorCatalog: cat, Objects: restorationObjects{}}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if _, err := coordinator.Admit(context.Background(), who, uuid.New(), project,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), 0, proposal, nil, pin,
		uuid.New(), uuid.New()); err != v2input.ErrInvalid || calls != 0 || len(tx.queries) != 0 {
		t.Fatal("catalog version mismatch reached SQL or external IO", err)
	}
}

type glareCountConfirmation struct{ calls *int }

func (p glareCountConfirmation) PublishDonorConfirmation(context.Context,
	v2portrait.DonorConfirmationPublication) (v2input.Ref, error) {
	(*p.calls)++
	return v2input.Ref{}, v2input.ErrInvalid
}

func TestGlareCoordinatorRejectsInvalidPlanBeforeExternalIO(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	pin := v2portrait.RestorationQualificationPin{Operation: "portrait.glare",
		OperationVersion: "1", PolicyID: uuid.New(), QualificationID: uuid.New(),
		PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha,
		RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
		IssuerID: uuid.New(), Backend: "cpu"}
	tx := &restorationStoreTx{role: "photo_scheduler", accepted: true}
	calls := 0
	cat := &restorationCatalog{objects: map[uuid.UUID]objectstore.Object{}, calls: map[uuid.UUID]int{}}
	coordinator := GlareDonorRestorationCoordinator{DB: restorationStoreDB{tx},
		ConfirmationPublisher: glareCountConfirmation{&calls},
		ParamsPublisher:       glareCountDocuments{&calls},
		TransportPublisher:    glareCountDocuments{&calls},
		BaseCatalog:           cat, DonorCatalog: cat, Objects: restorationObjects{}}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if _, err := coordinator.Admit(context.Background(), who, uuid.New(), uuid.New(),
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), 0,
		[]byte(`{"schema_version":1}`), []byte(`{"backend":"cpu"}`), pin,
		uuid.New(), uuid.New()); err == nil || calls != 0 || len(tx.queries) != 0 {
		t.Fatal("invalid profile reached issuer, catalog or SQL")
	}
}
