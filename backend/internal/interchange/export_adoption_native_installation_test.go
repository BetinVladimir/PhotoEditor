package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportNativeInstallationRequiresPinnedScopedAuthority(t *testing.T) {
	installation := SchedulerExportNativeInstallation{}
	if _, err := installation.BuildSchedulerExportNativeResolver(context.Background(),
		ExportAdoptionClaim{}, SchedulerExportResult{}, SQLSchedulerExportInputCatalog{}, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("empty native installation accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := installation.BuildSchedulerExportNativeResolver(ctx,
		ExportAdoptionClaim{}, SchedulerExportResult{}, SQLSchedulerExportInputCatalog{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled native installation proceeded", err)
	}
	if _, err := (NativeExportDeployment{}).BuildSchedulerExportAdoptionFromDeployment(
		context.Background(), nil, nil, "photos", nil, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("deployment without authority accepted", err)
	}
}
