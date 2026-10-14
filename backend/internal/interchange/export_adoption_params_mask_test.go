package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerParamsMaskAuthorityFailsBeforeIO(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	if err := catalog.QualifyExportParamsMask(context.Background(), v2input.Entry{}, nil, v2input.Ref{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled Params mask authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportParamsMask(ctx, v2input.Entry{}, nil, v2input.Ref{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Params mask authority: %v", err)
	}
}
