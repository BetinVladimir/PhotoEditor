package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerResolvedOriginFailsBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	entry := v2input.Entry{Contract: "ResolvedStyle", Ref: v2input.Ref{
		ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if _, err := catalog.LookupExportResolvedOrigin(context.Background(), entry, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled resolved origin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportResolvedOrigin(ctx, entry, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled resolved origin: %v", err)
	}
}
