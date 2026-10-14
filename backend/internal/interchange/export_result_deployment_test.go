package interchange

import (
	"context"
	"testing"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type exportResultAuthorityFactoryStub struct{}

func (exportResultAuthorityFactoryStub) BuildNativeExportAuthority(_ context.Context, native ExportNativeResolverInstallation) (ExportDocumentValidatorFactory, error) {
	if native.Scratch == nil || native.ImageProbe == nil || native.MaskProbe == nil || native.ArrayProbe == nil {
		return nil, v2input.ErrInvalid
	}
	return ExportStyleValidatorInstallation{}, nil
}

func TestNativeExportResultDeploymentOwnsPinnedRuntime(t *testing.T) {
	wire := nativeExportDeploymentFixture(t)
	wire.ScratchRoot = t.TempDir()
	raw, err := canonical.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := DecodeNativeExportDeployment(raw, rawSHA(raw))
	if err != nil {
		t.Fatal(err)
	}
	installer, err := deployment.BuildResultInstallerWithAuthority(context.Background(), statsParamsObjects{}, exportResultAuthorityFactoryStub{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if installer.Native == nil || installer.Native.Scratch != installer.Scratch || installer.MaxInputBytes != wire.MaxInputBytes || installer.ValidatorFactory == nil || installer.Binaries != nil {
		t.Fatal("result installer lost pinned native authority")
	}
	if installer.Native.Catalog != nil || installer.Native.JSON != nil || installer.Native.Objects != nil {
		t.Fatal("process installer contains request scope")
	}
}
