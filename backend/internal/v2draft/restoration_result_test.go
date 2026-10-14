package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestRestorationResultDocumentsRequireReviewAndGeneratedLineage(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	source, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	marshal := func(value any) []byte {
		raw, e := canonical.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	ref := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	sourceRaw := marshal(source)
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(sourceRaw)}
	base.CanvasHash, err = canonical.Hash(source.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	lock := strings.Repeat("b", 64)
	params := marshal(map[string]any{"schema_version": 1, "base": base, "model_lock_sha": lock})
	output := source
	output.Pixels = review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	imageRaw := marshal(output)
	imageRef := ref(imageRaw)
	changed := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("d", 64)},
		Canvas: source.Canvas, TransformChain: source.TransformChain, Selector: "changed", Confidence: 1}
	changedRaw := marshal(changed)
	changedRef := ref(changedRaw)
	generated := changed
	generated.Artifact.ID = uuid.New()
	generated.Artifact.SHA256 = strings.Repeat("e", 64)
	generated.Selector = "generated"
	generated.ModelLockID = &lock
	generatedRaw := marshal(generated)
	generatedRef := ref(generatedRaw)
	reason := "reflection_model_proposal"
	report := ImageReportDocument{SchemaVersion: 1, Status: "review", Operation: "image.reflection",
		Reason: &reason, Sources: []v2input.SessionVersionBinding{base}, Registrations: []v2input.Ref{}}
	reportRaw := marshal(report)
	reportRef := ref(reportRaw)
	result := ImageResultDocument{SchemaVersion: 1, WorkingImage: imageRef, ChangedMask: changedRef,
		Report: reportRef, GeneratedMask: &generatedRef}
	resultRaw := marshal(result)
	resultRef := ref(resultRaw)
	docs := RestorationResultDocuments{Source: sourceRaw, Result: resultRaw, Image: imageRaw,
		Report: reportRaw, Changed: changedRaw, Generated: generatedRaw}
	got, err := VerifyRestorationResultDocuments(context.Background(), "image.reflection", params, resultRef, docs)
	if err != nil || got.Pixels.ArtifactID != output.Pixels.ID || got.GeneratedPixels == nil ||
		got.GeneratedPixels.ArtifactID != generated.Artifact.ID {
		t.Fatal("review candidate rejected", err)
	}
	docs.Generated = nil
	if _, err := VerifyRestorationResultDocuments(context.Background(), "image.reflection", params, resultRef, docs); err == nil {
		t.Fatal("reflection missing generated mask accepted")
	}
	docs.Generated = generatedRaw
	report.Status = "pass"
	docs.Report = marshal(report)
	if _, err := VerifyRestorationResultDocuments(context.Background(), "image.reflection", params, resultRef, docs); err == nil {
		t.Fatal("non-review report accepted")
	}
	donor := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(sourceRaw), CanvasHash: base.CanvasHash}
	donorParams := marshal(map[string]any{"schema_version": 1, "base": base, "donor": donor,
		"mode": "donor", "model_lock_sha": lock})
	report.Status = "review"
	report.Operation = "portrait.glare"
	donorReason := "donor_review"
	report.Reason = &donorReason
	report.Sources = []v2input.SessionVersionBinding{base, donor}
	docs.Report = marshal(report)
	result.Report = ref(docs.Report)
	result.GeneratedMask = nil
	docs.Result = marshal(result)
	docs.Generated = nil
	donorResult := ref(docs.Result)
	if _, err := VerifyRestorationResultDocuments(context.Background(), "portrait.glare",
		donorParams, donorResult, docs); err != nil {
		t.Fatal("review-only donor glare rejected", err)
	}
	result.GeneratedMask = &generatedRef
	docs.Result = marshal(result)
	if _, err := VerifyRestorationResultDocuments(context.Background(), "portrait.glare",
		donorParams, ref(docs.Result), docs); err == nil {
		t.Fatal("donor glare accepted generated lineage")
	}
}
