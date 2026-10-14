package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
	"testing"
)

// Direct repository fixture verifies scope rejection only, not multi SQL authority.
func TestMultiQualityDocumentsRejectForeignScope(t *testing.T) {
	scope := artifactjson.Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params"}
	r := &MultiQualityDocuments{db: &pgxpool.Pool{}, scope: scope, sha: renderDerivativeBytesSHA([]byte("{}")), params: "{}", bucket: "photos"}
	foreign := scope
	foreign.Owner = uuid.New()
	if _, err := r.Reserve(context.Background(), foreign, r.sha, 2); err == nil {
		t.Fatal("foreign owner reached SQL")
	}
	if _, err := r.Complete(context.Background(), foreign, artifactjson.Reservation{}, objectstore.Object{}); err == nil {
		t.Fatal("foreign completion reached SQL")
	}
	if _, err := NewMultiQualityDocuments(nil, "photos", MultiQualityPlan{}, v2render.MultiResultContent{}); err == nil {
		t.Fatal("unverified repository accepted")
	}
}
func TestMultiQualityPublisherPreflight(t *testing.T) {
	if _, err := PublishMultiQualityTask(context.Background(), nil, nil, "photos", MultiQualityPlan{}, v2render.MultiResultContent{}); err == nil {
		t.Fatal("unconfigured publisher")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PublishMultiQualityTask(ctx, nil, nil, "photos", MultiQualityPlan{}, v2render.MultiResultContent{}); err != context.Canceled {
		t.Fatal(err)
	}
}
