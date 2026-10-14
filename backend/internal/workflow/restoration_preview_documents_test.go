package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

func TestRestorationPreviewDocumentsRefusesCrossSideAndScope(t *testing.T) {
	plan, source, before, _ := restorationPreviewFixture(t)
	doc := plan.Documents()[0]
	repo, err := NewRestorationPreviewDocuments(&pgxpool.Pool{}, source.Tenant,
		"photos", doc, before)
	if err != nil {
		t.Fatal(err)
	}
	if repo.scope.Owner != doc.ID || repo.side != "before" || repo.variant != "preview1600" {
		t.Fatal("incorrect publication scope")
	}
	bad := repo.scope
	bad.Tenant = uuid.New()
	if _, err = repo.Reserve(context.Background(), bad, repo.sha,
		int64(len(repo.params))); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal("foreign scope reached SQL", err)
	}
	if _, err = repo.Reserve(context.Background(), repo.scope, repo.sha,
		int64(len(repo.params))+1); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal("wrong byte count reached SQL", err)
	}
	if _, err = repo.Complete(context.Background(), repo.scope,
		artifactjson.Reservation{ArtifactID: uuid.New(), Token: uuid.New()},
		objectstore.Object{Key: "wrong", VersionID: "v1", SHA256: repo.sha,
			Bytes: int64(len(repo.params)), ContentType: "application/json"}); !errors.Is(err, objectstore.ErrIntegrity) {
		t.Fatal("wrong object key reached SQL", err)
	}
	badDoc := doc
	badDoc.Side = "after"
	if _, err = NewRestorationPreviewDocuments(&pgxpool.Pool{}, source.Tenant,
		"photos", badDoc, before); err == nil {
		t.Fatal("same task admitted for different side")
	}
}

func TestRestorationPreviewPublishRejectsMissingDependencies(t *testing.T) {
	plan, _, before, after := restorationPreviewFixture(t)
	if _, err := PublishRestorationPreviewTasks(context.Background(), nil, nil,
		"photos", plan, before, after); err == nil {
		t.Fatal("uninstalled publisher accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishRestorationPreviewTasks(ctx, nil, nil,
		"photos", plan, before, after); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled publication accepted", err)
	}
}
