package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
)

func TestLocalPortraitSubmissionConfigurationAndScope(t *testing.T) {
	scheduler, catalog := &pgxpool.Pool{}, &pgxpool.Pool{}
	publisher := localTaskPublisherFunc(func(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error) {
		t.Fatal("unexpected publication")
		return artifactjson.Ref{}, nil
	})
	tenant, project := uuid.New(), uuid.New()
	service := LocalPortraitSubmissionService{Scheduler: scheduler, Catalog: catalog, Objects: closureFixture{}, Documents: publisher, Bucket: "photos", Tenant: tenant, Project: project, RuntimeSHA: strings.Repeat("a", 64), PolicySHA: strings.Repeat("b", 64), MaxReservedBytes: 1 << 32}
	if !service.Valid() {
		t.Fatal("valid operator configuration rejected")
	}
	if _, err := service.SubmitProjectKeyed(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, uuid.New(), "stable", LocalPortraitClientSelection{}); err == nil {
		t.Fatal("foreign project accepted")
	}
	service.PolicySHA = "invalid"
	if service.Valid() {
		t.Fatal("invalid policy pin accepted")
	}
	service.PolicySHA = strings.Repeat("b", 64)
	service.Catalog = scheduler
	if service.Valid() {
		t.Fatal("scheduler and API roles shared a pool")
	}
	if _, err := (*LocalPortraitSubmissionService)(nil).ListMaskPairs(context.Background(), auth.Identity{}, tenant, project, GlareMaskPairQuery{}); err == nil {
		t.Fatal("uninstalled mask catalog accepted a request")
	}
}
