package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestSQLExportResultInstallerRequiresQualifiedPortsBeforeIO(t *testing.T) {
	installer := SQLExportResultInstaller{}
	if _, err := installer.InstallExportResultPreparation(context.Background(), SQLExportOutputCatalog{}, v2input.Ref{}); err == nil {
		t.Fatal("incomplete result qualification accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := installer.InstallExportResultPreparation(ctx, SQLExportOutputCatalog{}, v2input.Ref{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
