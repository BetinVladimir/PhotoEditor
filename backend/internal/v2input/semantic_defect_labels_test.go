package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestSemanticDefectLabelsExactSourceAndIdentity(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("images")
	}
	imageBody := images[0]
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(imageBody)
	canvas, _ := canonical.Hash(image.Canvas)
	binding := SessionVersionBinding{AssetVersionID: uuid.New(), Source: Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}, CanvasHash: canvas}
	raw, err = os.ReadFile("../review/testdata/defect-proposal-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if json.Unmarshal(raw, &document) != nil {
		t.Fatal("proposal")
	}
	document["image"] = binding.Source
	document["source_pixels"] = image.Pixels
	document["canvas"] = image.Canvas
	document["transform_chain"] = image.TransformChain
	labels := document["label_map"].(map[string]any)
	for _, value := range document["candidates"].([]any) {
		c := value.(map[string]any)
		c["id"], err = canonical.Hash(map[string]any{"version": "defect_candidate_v1", "source_sha256": image.Pixels.SHA256, "mask_sha256": labels["sha256"], "label": c["label"], "bbox": c["bbox"], "diameter": c["diameter"]})
		if err != nil {
			t.Fatal(err)
		}
	}
	body, err := canonical.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	ref := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	result, err := ResolveSemanticDefectLabels(context.Background(), ref, body, binding, imageBody)
	if err != nil || result.ArtifactID.String() != labels["artifact_id"] || result.SHA256 != labels["sha256"] {
		t.Fatal("valid proposal", result, err)
	}
	reportRaw, err := os.ReadFile("../review/testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if json.Unmarshal(reportRaw, &report) != nil {
		t.Fatal("report")
	}
	report["params"].(map[string]any)["image"] = binding.Source
	report["proposal"] = ref
	protection := report["protection"].(map[string]any)
	protection["canvas"] = image.Canvas
	protection["transform_chain"] = image.TransformChain
	reportBody, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	reportDigest := sha256.Sum256(reportBody)
	reportRef := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(reportDigest[:])}
	protectedChild, err := ResolveSemanticDefectProtection(context.Background(), reportRef, reportBody, ref, body, binding, imageBody)
	var childRef Ref
	childRaw, _ := json.Marshal(protection["artifact"])
	if json.Unmarshal(childRaw, &childRef) != nil || err != nil || protectedChild != childRef {
		t.Fatal("report protection", protectedChild, err)
	}
	paramsBody, err := canonical.Marshal(report["params"])
	if err != nil {
		t.Fatal(err)
	}
	paramsDigest := sha256.Sum256(paramsBody)
	paramsRef := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(paramsDigest[:])}
	verifiedChild, err := VerifySemanticDefectEvidence(context.Background(), paramsRef, paramsBody, reportRef, reportBody, ref, body, binding, imageBody)
	if err != nil || verifiedChild != childRef {
		t.Fatal("producer params binding", verifiedChild, err)
	}

	maskBody, maskPixels, err := BuildSemanticDefectProtectionDocument(context.Background(), paramsRef, paramsBody, reportRef, reportBody, ref, body, binding, imageBody)
	expectedMaskBody, marshalErr := canonical.Marshal(protection)
	if err != nil || marshalErr != nil || maskPixels != childRef || string(maskBody) != string(expectedMaskBody) {
		t.Fatal("original inline protection document changed", err, marshalErr)
	}
	maskDigest := sha256.Sum256(maskBody)
	resolvedMask, err := ResolveSemanticMaskPixels(Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(maskDigest[:])}, maskBody, binding, imageBody, true)
	if err != nil || resolvedMask != childRef {
		t.Fatal("derived protection cannot be consumed as MaskRef", err)
	}
	wrongParams := paramsRef
	wrongParams.SHA256 = reportRef.SHA256
	rejectedBody, rejectedPixels, err := BuildSemanticDefectProtectionDocument(context.Background(), wrongParams, paramsBody, reportRef, reportBody, ref, body, binding, imageBody)
	if err == nil || rejectedBody != nil || rejectedPixels != (Ref{}) {
		t.Fatal("mismatched params emitted protection document")
	}
	var changedParams map[string]any
	if json.Unmarshal(paramsBody, &changedParams) != nil {
		t.Fatal("params")
	}
	changedParams["diameter_face_fraction"] = []float64{.004, .012}
	changedBody, _ := canonical.Marshal(changedParams)
	changedDigest := sha256.Sum256(changedBody)
	changedRef := Ref{ArtifactID: paramsRef.ArtifactID, SHA256: hex.EncodeToString(changedDigest[:])}
	if child, err := VerifySemanticDefectEvidence(context.Background(), changedRef, changedBody, reportRef, reportBody, ref, body, binding, imageBody); err != ErrInvalid || child != (Ref{}) {
		t.Fatal("different valid producer params accepted", child, err)
	}
	wrongParamsRef := paramsRef
	wrongParamsRef.SHA256 = reportRef.SHA256
	if child, err := VerifySemanticDefectEvidence(context.Background(), wrongParamsRef, paramsBody, reportRef, reportBody, ref, body, binding, imageBody); err != ErrInvalid || child != (Ref{}) {
		t.Fatal("params hash substitution", child, err)
	}
	if child, err := VerifySemanticDefectEvidence(context.Background(), reportRef, paramsBody, reportRef, reportBody, ref, body, binding, imageBody); err != ErrInvalid || child != (Ref{}) {
		t.Fatal("aliased params and report", child, err)
	}
	protection["protected"] = false
	badReport, _ := canonical.Marshal(report)
	badDigest := sha256.Sum256(badReport)
	badReportRef := Ref{ArtifactID: reportRef.ArtifactID, SHA256: hex.EncodeToString(badDigest[:])}
	if child, err := ResolveSemanticDefectProtection(context.Background(), badReportRef, badReport, ref, body, binding, imageBody); err != ErrInvalid || child != (Ref{}) {
		t.Fatal("unprotected defect report accepted", child, err)
	}
	wrong := binding
	wrong.Source.ArtifactID = uuid.New()
	if child, err := ResolveSemanticDefectLabels(context.Background(), ref, body, wrong, imageBody); err != ErrInvalid || child != (Ref{}) {
		t.Fatal("foreign source accepted", child, err)
	}
	document["candidates"].([]any)[0].(map[string]any)["id"] = ref.SHA256
	bad, _ := canonical.Marshal(document)
	sum = sha256.Sum256(bad)
	badRef := Ref{ArtifactID: ref.ArtifactID, SHA256: hex.EncodeToString(sum[:])}
	if child, err := ResolveSemanticDefectLabels(context.Background(), badRef, bad, binding, imageBody); err != ErrInvalid || child != (Ref{}) {
		t.Fatal("forged candidate accepted", child, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if child, err := ResolveSemanticDefectLabels(ctx, ref, body, binding, imageBody); !errors.Is(err, context.Canceled) || child != (Ref{}) {
		t.Fatal("cancelled evidence accepted", child, err)
	}
}
