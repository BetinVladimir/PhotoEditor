package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultParamsMaskRequiresCanonicalInputCatalog(t *testing.T) {
	if err := (SQLExportResultInputCatalog{}).QualifyExportParamsMask(
		context.Background(), v2input.Entry{}, []byte("{}"), v2input.Ref{}); err != v2input.ErrInvalid {
		t.Fatal("inline mask accepted without canonical result catalog", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SQLExportResultInputCatalog{}).QualifyExportParamsMask(
		ctx, v2input.Entry{}, []byte("{}"), v2input.Ref{}); err != context.Canceled {
		t.Fatal("inline mask lost cancellation", err)
	}
}
