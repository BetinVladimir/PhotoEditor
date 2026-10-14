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

func TestPerspectivePreviewDocumentsOwnedEvidenceAndScope(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectivePreviewPlan(bundle, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	docs := plan.Documents()
	raw := bundle.Documents()
	result := bundle.Result()
	root, image := raw[plan.source.Result.ArtifactID.String()], raw[result.WorkingImage.ArtifactID.String()]
	repo, err := NewPerspectivePreviewDocuments(&pgxpool.Pool{}, plan.source.Tenant, "photos", docs[0], root, image)
	if err != nil {
		t.Fatal(err)
	}
	expected := repo.params
	docs[0].Params[0] = 'x'
	root[0] = 'x'
	image[0] = 'x'
	if repo.params != expected || repo.root[0] == 'x' || repo.image[0] == 'x' {
		t.Fatal("borrowed evidence")
	}
	foreign := repo.scope
	foreign.Owner = uuid.New()
	if _, err := repo.Reserve(context.Background(), foreign, repo.sha, int64(len(repo.params))); err == nil {
		t.Fatal("foreign scope reached SQL")
	}
	if _, err := repo.Complete(context.Background(), foreign, artifactjson.Reservation{}, objectstore.Object{}); err == nil {
		t.Fatal("foreign completion reached SQL")
	}
}
func TestPerspectivePreviewPublisherRefusesMissingDependenciesAndCancellation(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectivePreviewPlan(bundle, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublishPerspectivePreviewTasks(context.Background(), nil, nil, "photos", plan, bundle); err == nil {
		t.Fatal("missing IO dependencies accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishPerspectivePreviewTasks(ctx, nil, nil, "photos", plan, bundle); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
