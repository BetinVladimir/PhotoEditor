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

func TestReflectionSubmissionServiceRequiresOperatorOwnedPins(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	pin := v2portrait.RestorationQualificationPin{Operation: "image.reflection",
		OperationVersion: "1", PolicyID: uuid.New(), QualificationID: uuid.New(),
		PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha,
		RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
		IssuerID: uuid.New(), Backend: "cpu"}
	profile, err := canonical.Marshal(map[string]any{
		"backend": "cpu", "renderer_version": "v2.2",
		"resource_estimate": v2portrait.LocalResourceEstimate{
			PixelCount: 100, RAMBytes: 3 * 1024 * 1024 * 1024,
			ScratchBytes: 1024, OutputBytes: 1024}})
	if err != nil {
		t.Fatal(err)
	}
	scheduler, catalog := &pgxpool.Pool{}, &pgxpool.Pool{}
	objects := &objectstore.Store{}
	tenant, project := uuid.New(), uuid.New()
	service, err := NewReflectionSubmissionService(scheduler, catalog, objects,
		"photos", tenant, project, pin, profile, minRestorationDraftReservation)
	if err != nil || service.scheduler != scheduler || service.catalog != catalog ||
		service.pin != pin || service.reservedBytes != minRestorationDraftReservation {
		t.Fatal("valid installed reflection service refused", err)
	}
	if _, err := NewReflectionSubmissionService(scheduler, scheduler, objects,
		"photos", tenant, project, pin, profile, minRestorationDraftReservation); err != v2input.ErrInvalid {
		t.Fatal("shared scheduler/catalogue role accepted", err)
	}
	var absent *ReflectionSubmissionService
	if _, err := absent.SubmitProjectKeyed(context.Background(), auth.Identity{
		UserID: uuid.New(), SessionID: uuid.New()}, tenant, project,
		"reflection-1", ReflectionClientIntent{}); err != v2input.ErrInvalid {
		t.Fatal("uninstalled reflection submit accepted", err)
	}
}
