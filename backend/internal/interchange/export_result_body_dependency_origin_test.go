package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultBodyDependencyOriginRequiresCanonicalScope(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if _, err := catalog.LookupExportBodyDependencyOrigin(context.Background(), v2input.Entry{}, nil, v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("dependency origin accepted without canonical scope", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportBodyDependencyOrigin(ctx, v2input.Entry{}, nil, v2input.Entry{}); err != context.Canceled {
		t.Fatal("dependency origin lost cancellation", err)
	}
}
