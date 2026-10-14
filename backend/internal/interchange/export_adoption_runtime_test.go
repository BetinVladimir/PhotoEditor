package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportAdoptionRuntimeFailsClosedBeforeInstallation(t *testing.T) {
	if err := SchedulerExportAdoptionSQLReady(context.Background(), nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("nil SQL authority accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SchedulerExportAdoptionSQLReady(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled SQL authority queried", err)
	}
	if _, err := BuildSchedulerExportAdoptionProcessor(context.Background(), nil, nil,
		"photos", nil, nil, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("partial export installation accepted", err)
	}
	if _, err := BuildSchedulerExportAdoptionProcessor(ctx, nil, nil,
		"photos", nil, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled export installation proceeded", err)
	}
}
