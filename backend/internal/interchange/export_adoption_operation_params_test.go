package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerOperationParamsFailBeforeSQL(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	entry := v2input.Entry{Contract: "Params", Ref: v2input.Ref{
		ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if _, err := catalog.LookupExportParamsOperation(context.Background(), entry, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled params authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportParamsOperation(ctx, entry, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled params authority: %v", err)
	}
}
