package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestPerspectiveLinesRepositoryRejectsForeignScopeBeforeIO(t *testing.T) {
	tenant, run, owner := uuid.New(), uuid.New(), uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	p := PerspectiveLinesDraftSnapshot{Tenant: tenant, Run: run, Owner: owner, Actor: who.UserID, Session: who.SessionID, SHA256: strings.Repeat("a", 64), LinesJCS: "{}"}
	raw, err := canonical.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	r := perspectiveLinesRepository{bucket: "photos", tenant: tenant, who: who, plan: PerspectiveLinesDraft{raw: string(raw)}}
	scope := artifactjson.Scope{Tenant: tenant, Run: run, Owner: owner, Purpose: "task_params"}
	if _, err := r.Reserve(context.Background(), scope, p.SHA256, 2); err == nil {
		t.Fatal("missing database accepted")
	}
	scope.Owner = uuid.New()
	if _, err := r.Reserve(context.Background(), scope, p.SHA256, 2); err == nil {
		t.Fatal("foreign owner accepted")
	}
	scope.Owner = owner
	scope.AssemblyLease = uuid.New()
	if _, err := r.Reserve(context.Background(), scope, p.SHA256, 2); err == nil {
		t.Fatal("assembly lease accepted")
	}
	if _, err := PublishPerspectiveLines(context.Background(), nil, nil, "photos", who, tenant, r.plan); err == nil {
		t.Fatal("missing object writer accepted")
	}
}
