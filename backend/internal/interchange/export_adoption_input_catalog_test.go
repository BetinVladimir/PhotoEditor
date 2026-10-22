package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportInputCatalogFailsBeforeIO(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	entry := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(),
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Contract: "MaskRef"}
	if _, err := catalog.LookupExportDocument(context.Background(), entry); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled catalog: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.LookupExportDocument(ctx, entry); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled catalog: %v", err)
	}
}

func TestSchedulerExportInputContractAllowlist(t *testing.T) {
	for _, contract := range []string{"MaskRef", "WorkingImage", "EncodedImage", "DenseWarpArray",
		"ResolvedStyle", "FaceBoxes", "AbsoluteLUTArray", "DefectLabelMap", "DefectProposeReport", "DefectLabelPNG"} {
		if !validExportInputContract(contract) {
			t.Fatalf("rejected %s", contract)
		}
	}
	for _, contract := range []string{"", "PersonOriginReceipt", "InterchangeArchive", "Unknown"} {
		if validExportInputContract(contract) {
			t.Fatalf("accepted %s", contract)
		}
	}
}
