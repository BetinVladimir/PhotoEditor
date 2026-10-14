package review

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestNativeDefectFacesPreserveQualifiedBindings(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report proposalReportDocument
	if err := decodeExact(raw, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Params.Faces) < 2 {
		t.Fatal("fixture needs multiple faces")
	}
	source := DefectSource{Image: report.Params.Image, Pixels: ArtifactRef{ID: uuid.New(), SHA256: report.Params.Image.SHA256}, Canvas: report.Protection.Canvas, TransformChain: report.Protection.TransformChain}
	qualified := append([]DefectProposalFaceInput{}, report.Params.Faces...)
	verify := func(params proposalReportParams) error {
		body, err := canonical.Marshal(params)
		if err != nil {
			return err
		}
		digest, err := canonical.Hash(params)
		if err != nil {
			return err
		}
		return VerifyDefectQualifiedFaceBindings(context.Background(), ArtifactRef{ID: uuid.New(), SHA256: digest}, body, source, qualified)
	}
	if err := verify(report.Params); err != nil {
		t.Fatal(err)
	}
	dropped := report.Params
	dropped.Faces = dropped.Faces[:1]
	if err := verify(dropped); err == nil {
		t.Fatal("partial face coverage accepted")
	}
	changed := report.Params
	changed.Faces = append([]DefectProposalFaceInput{}, qualified...)
	changed.Faces[0].Landmarks.ID = uuid.New()
	if err := verify(changed); err == nil {
		t.Fatal("substituted landmark binding accepted")
	}
	reordered := report.Params
	reordered.Faces = append([]DefectProposalFaceInput{}, qualified...)
	reordered.Faces[0], reordered.Faces[1] = reordered.Faces[1], reordered.Faces[0]
	if err := verify(reordered); err == nil {
		t.Fatal("changed face ordering accepted")
	}
}
