package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestRestorationMaterialClosesSourceAndMaskArrays(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var originals []json.RawMessage
	if json.Unmarshal(fixture, &originals) != nil || len(originals) == 0 {
		t.Fatal("image fixture")
	}
	image, err := review.ParseWorkingImage(originals[0])
	if err != nil {
		t.Fatal(err)
	}
	sourceRaw, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	sourceRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(sourceRaw)}
	canvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: sourceRef, CanvasHash: canvasSHA}
	target := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: image.Pixels.SHA256},
		Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1}
	guard := target
	guard.Artifact.ID = uuid.New()
	guard.Protected = true
	targetRaw, err := canonical.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	guardRaw, err := canonical.Marshal(guard)
	if err != nil {
		t.Fatal(err)
	}
	targetRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(targetRaw)}
	guardRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(guardRaw)}
	pin := restorationPin("image.reflection")
	paramsRaw, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "base": base, "mask": targetRef, "protected": guardRef,
		"calibration": pin.Calibration, "model_lock_sha": pin.ModelLockSHA})
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(paramsRaw)}
	entries := []v2input.Entry{
		{Ref: sourceRef, Contract: "WorkingImage"}, {Ref: targetRef, Contract: "MaskRef"},
		{Ref: guardRef, Contract: "MaskRef"}, {Ref: pin.Calibration, Contract: "Calibration"},
		{Ref: paramsRef, Contract: "Params"},
		{Ref: pin.Report, Contract: "Report"},
		{Ref: v2input.Ref{ArtifactID: target.Artifact.ID, SHA256: target.Artifact.SHA256}, Contract: "NpyArray"},
		{Ref: v2input.Ref{ArtifactID: guard.Artifact.ID, SHA256: guard.Artifact.SHA256}, Contract: "NpyArray"},
	}
	arrays, err := restorationImageArrays(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range arrays {
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "NpyArray"})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	_, profile := restorationProfileFixture(t, pin)
	draft, err := CompileRestorationDraftTask(context.Background(), uuid.New(), 0,
		"image.reflection", paramsRaw, profile, pin, paramsRef, entries)
	if err != nil {
		t.Fatal(err)
	}
	docs := RestorationDocuments{Source: sourceRaw, Target: targetRaw, Protected: guardRaw}
	derived, err := BuildRestorationInputInventory(context.Background(),
		"image.reflection", paramsRaw, paramsRef, pin, docs)
	if err != nil || !reflect.DeepEqual(derived, entries) {
		t.Fatal("restoration inventory did not derive exact physical roots", err)
	}
	if err := VerifyRestorationMaterialInputs(context.Background(), draft, paramsRaw, docs); err != nil {
		t.Fatal("valid material refused", err)
	}
	verified, err := CompileVerifiedRestorationDraftTask(context.Background(), uuid.New(), 0,
		"image.reflection", paramsRaw, profile, pin, paramsRef, entries, docs)
	if err != nil || verified.Task.InputDigest != draft.Task.InputDigest {
		t.Fatal("publication compiler rejected closed inventory", err)
	}
	docs.Target = append([]byte(" "), targetRaw...)
	if err := VerifyRestorationMaterialInputs(context.Background(), draft, paramsRaw, docs); err == nil {
		t.Fatal("changed mask bytes accepted")
	}
	if _, err := CompileVerifiedRestorationDraftTask(context.Background(), uuid.New(), 0,
		"image.reflection", paramsRaw, profile, pin, paramsRef, entries, docs); err == nil {
		t.Fatal("publication compiler accepted changed material")
	}
	docs.Target = targetRaw
	missing := draft
	missing.Entries = append([]v2input.Entry(nil), draft.Entries...)
	for i, entry := range missing.Entries {
		if entry.Ref.ArtifactID == image.Pixels.ID {
			missing.Entries = append(missing.Entries[:i], missing.Entries[i+1:]...)
			break
		}
	}
	if err := VerifyRestorationMaterialInputs(context.Background(), missing, paramsRaw, docs); err == nil {
		t.Fatal("missing source pixels accepted")
	}
}
