package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func TestDefectProposalTaskCompilation(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report proposalReportDocument
	if err = decodeExact(raw, &report); err != nil {
		t.Fatal(err)
	}
	// The document-only fixture reuses placeholder IDs across binary roles.
	// A task requires distinct original artifact identities.
	if report.Params.Permanent != nil {
		report.Params.Permanent.Artifact.ID = uuid.New()
	}
	skinID := uuid.New()
	for i := range report.Params.Faces {
		if report.Params.Faces[i].Skin != nil {
			report.Params.Faces[i].Skin.Artifact.ID = skinID
		}
	}
	source := DefectSource{Pixels: ArtifactRef{ID: uuid.New(), SHA256: report.Params.Image.SHA256}, Image: report.Params.Image, Canvas: report.Protection.Canvas, TransformChain: report.Protection.TransformChain}
	build := func(faces []DefectProposalFaceInput, diameter [2]float64) (DefectProposalTaskInputs, error) {
		return BuildDefectProposalTaskInputs(context.Background(), source, faces, diameter, report.Params.Permanent, nil, nil)
	}
	result, err := build(report.Params.Faces, [2]float64{.002, .012})
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := ArtifactRef{ID: uuid.New(), SHA256: result.ParamsSHA256}
	if err := VerifyDefectProposalDeclaredInputs(paramsRef, result.Params, source, result.Inputs); err != nil {
		t.Fatal("complete original dependencies", err)
	}
	for i := range result.Inputs {
		missing := append([]ArtifactRef(nil), result.Inputs[:i]...)
		missing = append(missing, result.Inputs[i+1:]...)
		if VerifyDefectProposalDeclaredInputs(paramsRef, result.Params, source, missing) == nil {
			t.Fatal("missing native dependency accepted", i)
		}
	}
	changed := append([]byte(nil), result.Params...)
	changed[len(changed)-1] = ' '
	if VerifyDefectProposalDeclaredInputs(paramsRef, changed, source, result.Inputs) == nil {
		t.Fatal("changed original Params accepted")
	}
	var params proposalReportParams
	if err = decodeExact(result.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Protections == nil || len(*params.Protections) != 0 || len(params.Faces) != len(report.Params.Faces) {
		t.Fatal("incomplete task Params")
	}
	expected := report.Params
	empty := []ProtectionMask{}
	expected.Protections = &empty
	wire, err := canonical.Marshal(expected)
	if err != nil || string(wire) != string(result.Params) {
		t.Fatal("worker Params changed", err)
	}
	sum := sha256.Sum256(result.Params)
	if result.ParamsSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("wrong Params digest")
	}
	required := map[ArtifactRef]bool{source.Image: true, source.Pixels: true}
	for _, face := range report.Params.Faces {
		required[face.Landmarks] = true
		for _, mask := range []*ProtectionMask{face.Skin, face.Manual} {
			if mask != nil {
				required[mask.Artifact] = true
			}
		}
	}
	if report.Params.Permanent != nil {
		required[report.Params.Permanent.Artifact] = true
	}
	if len(required) != len(result.Inputs) {
		t.Fatal("incomplete dependency inventory")
	}
	for i, ref := range result.Inputs {
		if !required[ref] || (i > 0 && result.Inputs[i-1].ID.String() >= ref.ID.String()) {
			t.Fatal("invalid dependency/order")
		}
	}
	// Returned serialized bytes must survive mutations of caller-owned masks.
	if len(report.Params.Faces) > 0 && report.Params.Faces[0].Skin != nil {
		report.Params.Faces[0].Skin.Confidence = .123
	}
	if string(wire) != string(result.Params) {
		t.Fatal("shared task Params")
	}
	for _, diameter := range [][2]float64{{.001, .012}, {.012, .002}, {.002, .013}} {
		denied, err := build(report.Params.Faces, diameter)
		if err == nil || denied.Params != nil || denied.Inputs != nil {
			t.Fatal("invalid diameter accepted")
		}
	}
	if denied, err := build(nil, [2]float64{.002, .012}); err == nil || denied.Params != nil {
		t.Fatal("null faces accepted")
	}
	for _, pixels := range []ArtifactRef{{}, source.Image, report.Params.Faces[0].Landmarks, report.Params.Faces[0].Skin.Artifact} {
		invalidSource := source
		invalidSource.Pixels = pixels
		denied, err := BuildDefectProposalTaskInputs(context.Background(), invalidSource, report.Params.Faces, [2]float64{.002, .012}, nil, nil, nil)
		if err == nil || denied.Params != nil || denied.Inputs != nil || denied.ManualRoots != nil || denied.ParamsSHA256 != "" {
			t.Fatal("invalid/aliased source pixels accepted", denied, err)
		}
	}
	faces := append([]DefectProposalFaceInput{}, report.Params.Faces...)
	faces[0].Landmarks = source.Image
	if denied, err := build(faces, [2]float64{.002, .012}); err == nil || denied.Inputs != nil {
		t.Fatal("cross-role source alias accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if denied, err := BuildDefectProposalTaskInputs(ctx, source, report.Params.Faces, [2]float64{.002, .012}, nil, nil, nil); !errors.Is(err, context.Canceled) || denied.Params != nil {
		t.Fatal("cancelled compilation accepted")
	}
}

func TestDefectTaskSourceOriginalImage(t *testing.T) {
	for _, body := range imageFixtures(t) {
		image, err := ParseWorkingImage(body)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		result, err := BuildDefectProposalTaskInputsFromImage(context.Background(), ref, body, []DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
		var chain transformChain
		if json.Unmarshal(image.TransformChain, &chain) != nil {
			t.Fatal("invalid fixture chain")
		}
		required := map[ArtifactRef]bool{ref: true, image.Pixels: true}
		if image.Alpha != nil {
			required[*image.Alpha] = true
		}
		for _, step := range chain.Steps {
			if step.InverseField != nil {
				required[*step.InverseField] = true
			}
		}
		if err != nil || len(result.Inputs) != len(required) {
			t.Fatal(result, err)
		}
		for _, input := range result.Inputs {
			if !required[input] {
				t.Fatal("unexpected source dependency")
			}
		}
		found := false
		for _, input := range result.Inputs {
			if input == image.Pixels {
				found = true
			}
		}
		if !found {
			t.Fatal("original RGB child missing")
		}
		changed := append(append([]byte{}, body...), ' ')
		if denied, err := BuildDefectProposalTaskInputsFromImage(context.Background(), ref, changed, []DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil); err == nil || denied.Params != nil {
			t.Fatal("changed original source accepted")
		}
		image.DType = "uint16"
		unsupported, err := canonical.Marshal(image)
		if err != nil {
			t.Fatal(err)
		}
		sum = sha256.Sum256(unsupported)
		ref.SHA256 = hex.EncodeToString(sum[:])
		if denied, err := BuildDefectProposalTaskInputsFromImage(context.Background(), ref, unsupported, []DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil); err == nil || denied.Inputs != nil {
			t.Fatal("uint16 source accepted")
		}
	}
}
