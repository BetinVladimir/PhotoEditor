package v2optical

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestPerspectiveLinesCatalogInvalidScopeBeforeSQL(t *testing.T) {
	catalog := SQLPerspectiveLinesCatalog{Bucket: "photos"}
	p := perspectiveIntent()
	scope := PerspectiveLinesScope{Tenant: uuid.New(), Project: uuid.New(), Snapshot: uuid.New(), Actor: uuid.New(), Session: uuid.New(), Base: p.Base, Lines: p.Base.Source}
	if _, err := catalog.ResolvePerspectiveLines(context.Background(), scope); err == nil {
		t.Fatal("missing database accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.ResolvePerspectiveLines(ctx, scope); err != context.Canceled {
		t.Fatal(err)
	}
}
