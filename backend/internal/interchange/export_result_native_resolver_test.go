package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestExportResultNativeResolverBindsCanonicalRoot(t *testing.T) {
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("result"))}
	catalog := SQLExportResultInputCatalog{Root: root}
	template := &ExportNativeResolverInstallation{}
	preparation := ExportResultPreparation{
		Objects: statsParamsObjects{}, Validator: exportReaderValidator{&importValidatorStub{}},
		Native: template, MaxInputBytes: 1 << 20,
	}
	resolver, err := preparation.resultInventoryResolver(context.Background(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	native, ok := resolver.(ExportNativeResolverInstallation)
	if !ok || native.MaxBytes != preparation.MaxInputBytes {
		t.Fatal("result native resolver was not selected")
	}
	bound, ok := native.Catalog.(SQLExportResultInputCatalog)
	if !ok || bound.Root != root {
		t.Fatal("native catalog lost canonical result root")
	}
	json, ok := native.JSON.(ExportJSONNodeResolver)
	if !ok || json.Catalog == nil {
		t.Fatal("native JSON resolver lacks result catalog")
	}
	if template.Catalog != nil || template.JSON != nil || template.MaxBytes != 0 {
		t.Fatal("process native template was mutated")
	}
}
