package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestDefectSourceCompilerRetainsAlphaAndDenseGeometryWithoutChangingParams(t *testing.T) {
	raw, err := os.ReadFile("testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	seenWarp := false
	for _, body := range images {
		image, err := ParseWorkingImage(body)
		if err != nil {
			t.Fatal(err)
		}
		if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" {
			continue
		}
		alpha := ArtifactRef{ID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
		image.Alpha = &alpha
		body, err = canonical.Marshal(image)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		base, err := BuildDefectProposalTaskInputs(context.Background(), image.Source(ref), []DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := BuildDefectProposalTaskInputsFromImage(context.Background(), ref, body, []DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(base.Params) != string(compiled.Params) || base.ParamsSHA256 != compiled.ParamsSHA256 {
			t.Fatal("source arrays changed processing Params")
		}
		selected := map[ArtifactRef]bool{}
		for _, input := range compiled.Inputs {
			selected[input] = true
		}
		if !selected[alpha] {
			t.Fatal("alpha omitted")
		}
		var chain transformChain
		if err := json.Unmarshal(image.TransformChain, &chain); err != nil {
			t.Fatal(err)
		}
		conflict := image
		conflict.Alpha = &image.Pixels
		conflictBody, _ := canonical.Marshal(conflict)
		conflictSum := sha256.Sum256(conflictBody)
		conflictRef := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(conflictSum[:])}
		if rejected, err := BuildDefectProposalTaskInputsFromImage(context.Background(), conflictRef, conflictBody, []DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil); err == nil || rejected.Params != nil || rejected.Inputs != nil {
			t.Fatal("RGB reused as alpha accepted")
		}
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		if rejected, err := BuildDefectProposalTaskInputsFromImage(cancelled, ref, body, []DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil); !errors.Is(err, context.Canceled) || rejected.Params != nil {
			t.Fatal("cancelled source compilation accepted", err)
		}
		for _, step := range chain.Steps {
			if step.InverseField != nil {
				seenWarp = true
				conflict := image
				conflictingAlpha := *step.InverseField
				conflictingAlpha.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
				conflict.Alpha = &conflictingAlpha
				conflictBody, _ := canonical.Marshal(conflict)
				conflictSum := sha256.Sum256(conflictBody)
				conflictRef := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(conflictSum[:])}
				if rejected, err := BuildDefectProposalTaskInputsFromImage(context.Background(), conflictRef, conflictBody, []DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil); err == nil || rejected.Params != nil || rejected.Inputs != nil {
					t.Fatal("conflicting alpha/warp digest accepted")
				}

				if !selected[*step.InverseField] {
					t.Fatal("dense warp omitted")
				}
			}
		}
	}
	if !seenWarp {
		t.Fatal("dense geometry fixture not exercised")
	}
}
