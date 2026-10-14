package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultResolvedOriginRequiresCanonicalInputCatalog(t *testing.T) {
	if _, err := (SQLExportResultInputCatalog{}).LookupExportResolvedOrigin(
		context.Background(), v2input.Entry{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("resolved origin accepted without canonical result catalog", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLExportResultInputCatalog{}).LookupExportResolvedOrigin(
		ctx, v2input.Entry{}, []byte("{}")); err != context.Canceled {
		t.Fatal("resolved origin lost cancellation", err)
	}
}
