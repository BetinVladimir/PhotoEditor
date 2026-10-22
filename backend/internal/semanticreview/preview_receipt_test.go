package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestPreviewReceiptMatchesExactCanonicalAttempt(t *testing.T) {
	var linkage PreviewPresentationLinkage
	linkage.Document.TaskID = uuid.New()
	linkage.Document.ResultID = uuid.New()
	linkage.Document.Attempt = 2
	linkage.Document.Fence = 3
	digest := previewParamsFixture().Plan.SHA256
	receipt := ExposurePreviewReceipt{SchemaVersion: 1, ParamsSHA: digest, TaskID: linkage.Document.TaskID,
		ResultID: linkage.Document.ResultID, Attempt: 2, Fence: 3}
	for _, change := range []string{"valid", "attempt", "fence", "result", "params", "schema"} {
		value := receipt
		switch change {
		case "attempt":
			value.Attempt++
		case "fence":
			value.Fence++
		case "result":
			value.ResultID = uuid.New()
		case "params":
			value.ParamsSHA = ""
		case "schema":
			value.SchemaVersion = 2
		}
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		_, err = decodePreviewReceipt(raw, linkage, digest)
		if (err == nil) != (change == "valid") {
			t.Fatal(change, err)
		}
	}
}
