package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRelightSourcePhysicalInputs(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("fixture")
	}
	coveredWarp := false
	for _, original := range images {
		image, err := review.ParseWorkingImage(original)
		if err != nil {
			t.Fatal(err)
		}
		image.Alpha = &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("f", 64)}
		raw, err := canonical.Marshal(image)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		canvas, _ := canonical.Hash(image.Canvas)
		ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
		p := RelightParams{SchemaVersion: 1, Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}, Depth: ref(), Subject: ref(), Protected: ref(), Calibration: ref(), Lights: []RelightLight{{Radius: .5}}}
		pin := RelightQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: strings.Repeat("a", 64), Report: ref(), Calibration: p.Calibration, ModelLockSHA: strings.Repeat("a", 64), RuntimeLockSHA: strings.Repeat("a", 64), DeviceProfileSHA: strings.Repeat("a", 64), DatasetSHA: strings.Repeat("a", 64), IssuerID: uuid.New(), Backend: "cpu"}
		depth := review.RelativeDepth{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("d", 64)}, Float32Shape: []int64{image.Canvas.Height, image.Canvas.Width}, InvalidMask: &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("e", 64)}, Canvas: image.Canvas, TransformChain: image.TransformChain, Normalization: "minmax_valid"}
		depthRaw, _ := canonical.Marshal(depth)
		depthSum := sha256.Sum256(depthRaw)
		p.Depth.SHA256 = hex.EncodeToString(depthSum[:])
		depthInputs, err := RelightDepthInputs(p, pin, raw, depthRaw)
		if err != nil || len(depthInputs) != 2 {
			t.Fatal("depth dependencies", err)
		}
		for _, mutate := range []func(*review.RelativeDepth){
			func(d *review.RelativeDepth) { d.Float32Shape = []int64{1, 1} },
			func(d *review.RelativeDepth) { d.Normalization = "metric" },
			func(d *review.RelativeDepth) { d.InvalidMask = &d.Artifact },
			func(d *review.RelativeDepth) { d.Artifact.ID = p.Subject.ArtifactID },
			func(d *review.RelativeDepth) { d.Canvas.SpaceID = "foreign" },
		} {
			bad := depth
			mutate(&bad)
			badRaw, _ := canonical.Marshal(bad)
			badSum := sha256.Sum256(badRaw)
			badParams := p
			badParams.Depth.SHA256 = hex.EncodeToString(badSum[:])
			if _, err := RelightDepthInputs(badParams, pin, raw, badRaw); err == nil {
				t.Fatal("invalid depth accepted")
			}
		}
		mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("d", 64)}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "person", Confidence: 1}
		subjectRaw, err := canonical.Marshal(mask)
		if err != nil {
			t.Fatal(err)
		}
		mask.Protected = true
		mask.Artifact.ID = uuid.New()
		mask.Selector = "manual"
		guardRaw, err := canonical.Marshal(mask)
		if err != nil {
			t.Fatal(err)
		}
		subjectSum := sha256.Sum256(subjectRaw)
		guardSum := sha256.Sum256(guardRaw)
		p.Subject.SHA256 = hex.EncodeToString(subjectSum[:])
		p.Protected.SHA256 = hex.EncodeToString(guardSum[:])
		maskInputs, err := RelightMaskInputs(p, pin, raw, subjectRaw, guardRaw)
		if err != nil || len(maskInputs) != 2 {
			t.Fatal("mask dependencies", err)
		}
		for _, entry := range maskInputs {
			if entry.Contract != "NpyArray" {
				t.Fatal("mask contract")
			}
		}
		paramsRaw, _, err := p.Compile()
		if err != nil {
			t.Fatal(err)
		}
		capacity, _ := LocalPortraitResources(image.Canvas.Width, image.Canvas.Height)
		documents := RelightInputDocuments{Source: raw, Depth: depthRaw, Subject: subjectRaw, Protected: guardRaw}
		combined, err := CompileRelightInputPlan(context.Background(), p.Base, documents, paramsRaw, pin.RuntimeLockSHA, capacity, pin)
		if err != nil {
			t.Fatal("combined inventory", err)
		}
		expectedCount := 6 + 2 + 2 + 2
		var chainCount struct {
			Steps []struct {
				Kind string `json:"kind"`
			} `json:"steps"`
		}
		_ = json.Unmarshal(image.TransformChain, &chainCount)
		for _, step := range chainCount.Steps {
			if step.Kind == "dense_warp" {
				expectedCount++
			}
		}
		if len(combined.Inputs) != expectedCount {
			t.Fatal("incomplete combined inventory", len(combined.Inputs), expectedCount)
		}
		// Individually valid depth and mask refs cannot disagree about one UUID.
		conflict := depth
		conflict.Artifact.ID = mask.Artifact.ID
		conflict.Artifact.SHA256 = strings.Repeat("c", 64)
		conflictRaw, _ := canonical.Marshal(conflict)
		conflictSum := sha256.Sum256(conflictRaw)
		conflictParams := p
		conflictParams.Depth.SHA256 = hex.EncodeToString(conflictSum[:])
		conflictParamsRaw, _, _ := conflictParams.Compile()
		conflictDocuments := documents
		conflictDocuments.Depth = conflictRaw
		if _, err := CompileRelightInputPlan(context.Background(), p.Base, conflictDocuments, conflictParamsRaw, pin.RuntimeLockSHA, capacity, pin); err == nil {
			t.Fatal("cross-document hash conflict")
		}
		if _, err := RelightMaskInputs(p, pin, raw, guardRaw, subjectRaw); err == nil {
			t.Fatal("swapped masks")
		}
		changedMask := mask
		changedMask.Protected = false
		wrongRole, _ := canonical.Marshal(changedMask)
		wrongSum := sha256.Sum256(wrongRole)
		wrongParams := p
		wrongParams.Protected.SHA256 = hex.EncodeToString(wrongSum[:])
		if _, err := RelightMaskInputs(wrongParams, pin, raw, subjectRaw, wrongRole); err == nil {
			t.Fatal("unprotected guard")
		}
		changedMask = mask
		changedMask.Canvas.SpaceID = "foreign"
		wrongGeometry, _ := canonical.Marshal(changedMask)
		wrongSum = sha256.Sum256(wrongGeometry)
		wrongParams = p
		wrongParams.Protected.SHA256 = hex.EncodeToString(wrongSum[:])
		if _, err := RelightMaskInputs(wrongParams, pin, raw, subjectRaw, wrongGeometry); err == nil {
			t.Fatal("foreign geometry")
		}
		entries, err := RelightSourceInputs(p, pin, raw)
		if err != nil {
			t.Fatal(err)
		}
		expected := map[uuid.UUID]string{image.Pixels.ID: image.Pixels.SHA256, image.Alpha.ID: image.Alpha.SHA256}
		var chain struct {
			Steps []struct {
				Kind         string       `json:"kind"`
				InverseField *v2input.Ref `json:"inverse_field"`
			} `json:"steps"`
		}
		if json.Unmarshal(image.TransformChain, &chain) != nil {
			t.Fatal("chain")
		}
		for _, step := range chain.Steps {
			if step.Kind == "dense_warp" {
				coveredWarp = true
				expected[step.InverseField.ArtifactID] = step.InverseField.SHA256
			}
		}
		if len(entries) != len(expected) {
			t.Fatal("incomplete source inventory")
		}
		for i, entry := range entries {
			if entry.Contract != "NpyArray" || expected[entry.Ref.ArtifactID] != entry.Ref.SHA256 {
				t.Fatal("wrong physical ref")
			}
			if i > 0 && entries[i-1].Ref.ArtifactID.String() >= entry.Ref.ArtifactID.String() {
				t.Fatal("order")
			}
		}
		bad := p
		bad.Depth = v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
		if _, err := RelightSourceInputs(bad, pin, raw); err == nil {
			t.Fatal("root/pixel alias")
		}
		if _, err := RelightSourceInputs(p, pin, append(append([]byte{}, raw...), ' ')); err == nil {
			t.Fatal("changed source")
		}
	}
	if !coveredWarp {
		t.Fatal("fixture lost dense-warp coverage")
	}
}
