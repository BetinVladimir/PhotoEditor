package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

func TestPerspectiveQualityDocumentsOwnEvidenceAndDenyForeignScope(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectiveQualityPlan(bundle)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewPerspectiveQualityDocuments(&pgxpool.Pool{}, "photos", plan, bundle)
	if err != nil {
		t.Fatal(err)
	}
	original := repository.params
	plan.params[0] = 'x'
	if repository.params != original {
		t.Fatal("mutable Params exposed")
	}
	foreign := repository.scope
	foreign.Tenant = uuid.New()
	if _, err := repository.Reserve(context.Background(), foreign, repository.sha, int64(len(original))); err == nil {
		t.Fatal("foreign reservation accepted")
	}
	if _, err := repository.Complete(context.Background(), foreign, artifactjson.Reservation{ArtifactID: uuid.New(), Token: uuid.New()}, objectstore.Object{}); err == nil {
		t.Fatal("foreign completion accepted")
	}
}
func TestPerspectiveQualityDocumentsCompareExactCompilerInputsBeforeIO(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	good, err := CompilePerspectiveQualityPlan(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bad := good
	bad.inputs = good.Inputs()
	bad.inputs[1].ID = uuid.New()
	bad.inputs[1].SHA256 = strings.Repeat("e", 64)
	if _, err := NewPerspectiveQualityDocuments(&pgxpool.Pool{}, "photos", bad, bundle); err == nil {
		t.Fatal("foreign pixel input accepted")
	}
}
func TestPerspectiveQualityPublicationPreflightAndCancellation(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectiveQualityPlan(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPerspectiveQualityDocuments(nil, "photos", plan, bundle); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := PublishPerspectiveQualityTask(context.Background(), nil, nil, "photos", plan, bundle); err == nil {
		t.Fatal("missing objects accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishPerspectiveQualityTask(ctx, nil, nil, "photos", plan, bundle); err != context.Canceled {
		t.Fatal(err)
	}
}
