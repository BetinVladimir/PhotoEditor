package interchange

import (
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestExportPolicyCatalogConfigPreservesResultScope(t *testing.T) {
	result := SQLExportResultInputCatalog{Export: SQLExportOutputCatalog{Key: "request-key", Bucket: "objects"}}
	config, err := exportPolicyCatalogConfig(result)
	if err != nil || config.Key != result.Export.Key || config.Bucket != result.Export.Bucket {
		t.Fatal("result policy scope changed", err)
	}
	if _, err := exportPolicyCatalogConfig(nil); err != v2input.ErrInvalid {
		t.Fatal("missing policy catalog accepted", err)
	}
}
