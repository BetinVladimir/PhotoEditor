package interchange

import (
	"context"
	"testing"
)

func TestSQLExportResultInstallerRequiresQualifiedPortsBeforeIO(t *testing.T) {
	installer := SQLExportResultInstaller{}
	if _, err := installer.InstallExportResultPreparation(context.Background(), SQLExportOutputCatalog{}); err == nil {
		t.Fatal("incomplete result qualification accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := installer.InstallExportResultPreparation(ctx, SQLExportOutputCatalog{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
