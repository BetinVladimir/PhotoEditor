package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
	"strings"
	"testing"
)

// Repository ownership is tested with generic media derivative bytes. This does
// not pretend to establish multi-specific relational authority or SQL success.
func TestMultiPreviewDocumentsOwnBytesAndRejectForeignScope(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	generic, err := CompilePerspectivePreviewPlan(bundle, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	doc := generic.Documents()[0]
	raw := bundle.Documents()
	root := raw[bundle.Source().Result.ArtifactID.String()]
	image := raw[bundle.Result().WorkingImage.ArtifactID.String()]
	repo, err := NewMultiPreviewDocuments(&pgxpool.Pool{}, bundle.Source().Tenant, "photos", doc, root, image)
	if err != nil {
		t.Fatal(err)
	}
	params := repo.params
	doc.Params[0] = 'x'
	root[0] = 'x'
	image[0] = 'x'
	if repo.params != params || repo.root[0] == 'x' || repo.image[0] == 'x' {
		t.Fatal("repository aliases caller bytes")
	}
	foreign := repo.scope
	foreign.Owner = uuid.New()
	if _, err := repo.Reserve(context.Background(), foreign, repo.sha, int64(len(repo.params))); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal("foreign reserve reached SQL", err)
	}
	if _, err := repo.Complete(context.Background(), foreign, artifactjson.Reservation{}, objectstore.Object{}); !errors.Is(err, objectstore.ErrIntegrity) {
		t.Fatal("foreign complete reached SQL", err)
	}
}
func TestMultiPreviewPublisherRefusesMissingIOAndCancelledContext(t *testing.T) {
	if _, err := PublishMultiPreviewTasks(context.Background(), nil, nil, "photos", MultiPreviewPlan{}, v2render.MultiResultContent{}); err == nil {
		t.Fatal("unconfigured publisher")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishMultiPreviewTasks(ctx, nil, nil, "photos", MultiPreviewPlan{}, v2render.MultiResultContent{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}
