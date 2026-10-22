package semanticreview

import (
	"encoding/json"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRetouchPreviewReceiptMatchesExactCanonicalAttempt(t *testing.T) {
	var linkage RetouchPreviewPresentationLinkage
	linkage.Document.TaskID = uuid.New()
	linkage.Document.ResultID = uuid.New()
	linkage.Document.Attempt = 2
	linkage.Document.Fence = 3
	digest := previewParamsFixture().Plan.SHA256
	newRef := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest} }
	linkage.Document.Source.Ref = newRef()
	linkage.Presentation.WorkingImage = newRef()
	linkage.Presentation.Pixels = newRef()
	linkage.Presentation.NumericalEvidence = newRef()
	receipt := RetouchPreviewReceipt{SchemaVersion: 1, ParamsSHA: digest, TaskID: linkage.Document.TaskID,
		ResultID: linkage.Document.ResultID, Attempt: 2, Fence: 3, Presentation: linkage.Document.Source.Ref, WorkingImage: linkage.Presentation.WorkingImage, Pixels: linkage.Presentation.Pixels, NumericalEvidence: linkage.Presentation.NumericalEvidence}
	for _, change := range []string{"valid", "attempt", "fence", "result", "task", "presentation", "image", "pixels", "evidence", "alias", "params", "schema"} {
		value := receipt
		switch change {
		case "attempt":
			value.Attempt++
		case "fence":
			value.Fence++
		case "result":
			value.ResultID = uuid.New()
		case "task":
			value.TaskID = uuid.New()
		case "presentation":
			value.Presentation = newRef()
		case "image":
			value.WorkingImage = newRef()
		case "pixels":
			value.Pixels = newRef()
		case "evidence":
			value.NumericalEvidence = newRef()
		case "alias":
			value.Pixels = value.WorkingImage
		case "params":
			value.ParamsSHA = ""
		case "schema":
			value.SchemaVersion = 2
		}
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		_, err = decodeRetouchPreviewReceipt(raw, linkage, digest)
		if (err == nil) != (change == "valid") {
			t.Fatal(change, err)
		}
	}
}

func TestRetouchReceiptRejectsUnverifiedBeforeDatabaseIO(t *testing.T) {
	_, err := SealRetouchPreviewReceipt(context.Background(), nil, auth.Identity{}, uuid.New(), PhysicalRetouchPreview{})
	if err != ErrInvalid {
		t.Fatal(err)
	}
}

func TestRetouchReceiptRejectsMalformedTransport(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(`{}`), []byte(`null`), []byte(`[]`), []byte(`{"schema_version":true}`), []byte(`{"schema_version":1,"unexpected":true}`), make([]byte,16385)} {
		got,err := decodeRetouchPreviewReceipt(raw,RetouchPreviewPresentationLinkage{},previewParamsFixture().Plan.SHA256)
		if err!=ErrInvalid || got.SchemaVersion!=0 { t.Fatal("malformed receipt accepted",len(raw),err) }
	}
}

func TestRetouchReceiptCancellationBeforeDatabaseIO(t *testing.T) {
	ctx,cancel := context.WithCancel(context.Background())
	cancel()
	got,err := SealRetouchPreviewReceipt(ctx,nil,auth.Identity{},uuid.New(),PhysicalRetouchPreview{})
	if err!=context.Canceled || got.SchemaVersion!=0 { t.Fatal("cancelled receipt escaped",err) }
}

func TestRetouchPreviewReceiptPreservesSQLInt64Maximum(t *testing.T) {
 digest := previewParamsFixture().Plan.SHA256
 newRef := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256:digest} }
 var linkage RetouchPreviewPresentationLinkage
 linkage.Document.TaskID=uuid.New(); linkage.Document.ResultID=uuid.New()
 linkage.Document.Attempt=2147483647; linkage.Document.Fence=9223372036854775807
 linkage.Document.Source.Ref=newRef(); linkage.Presentation.WorkingImage=newRef()
 linkage.Presentation.Pixels=newRef(); linkage.Presentation.NumericalEvidence=newRef()
 receipt := RetouchPreviewReceipt{SchemaVersion:1,ParamsSHA:digest,
 TaskID:linkage.Document.TaskID,ResultID:linkage.Document.ResultID,
 Attempt:linkage.Document.Attempt,Fence:linkage.Document.Fence,
 Presentation:linkage.Document.Source.Ref,WorkingImage:linkage.Presentation.WorkingImage,
 Pixels:linkage.Presentation.Pixels,NumericalEvidence:linkage.Presentation.NumericalEvidence}
 // SQL jsonb is not an RFC8785 artifact. Decode directly into int64, no float.
 raw,err:=json.Marshal(receipt)
 if err!=nil { t.Fatal(err) }
 decoded,err:=decodeRetouchPreviewReceipt(raw,linkage,digest)
 if err!=nil || decoded.Fence!=receipt.Fence { t.Fatal("SQL fence changed",err) }
}
