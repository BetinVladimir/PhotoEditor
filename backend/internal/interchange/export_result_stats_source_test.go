package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportResultStatsRequiresCanonicalInputs(t *testing.T) {
	catalog := SQLExportResultInputCatalog{}
	if err := catalog.QualifyExportStats(context.Background(), v2input.Entry{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("Stats20 accepted without canonical input catalog", err)
	}
	if _, err := catalog.LookupExportStatsOrigin(context.Background(), v2input.Entry{}, []byte("{}")); err != v2input.ErrInvalid {
		t.Fatal("Stats20 origin accepted without canonical input catalog", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportStatsOrigin(ctx, v2input.Entry{}, []byte("{}")); err != context.Canceled {
		t.Fatal("Stats20 origin lost cancellation", err)
	}
}
