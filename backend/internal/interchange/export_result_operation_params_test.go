package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultOperationParamsRequiresCanonicalInputCatalog(t *testing.T) {
	if _, err := (SQLExportResultInputCatalog{}).LookupExportParamsOperation(
		context.Background(), v2input.Entry{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("operation Params accepted without canonical result catalog", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLExportResultInputCatalog{}).LookupExportParamsOperation(
		ctx, v2input.Entry{}, []byte("{}")); err != context.Canceled {
		t.Fatal("operation Params lost cancellation", err)
	}
}
