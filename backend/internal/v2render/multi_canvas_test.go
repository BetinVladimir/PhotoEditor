package v2render

import (
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiCanvasBindsEveryMatrixAndField(t *testing.T) {
	binding, _, _, _ := originalInventoryFixture(t, "optical.panorama")
	frames, reference, _, err := binding.Inputs()
	if err != nil {
		t.Fatal(err)
	}
	matrix := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	regs := []MultiRegistration{{Source: frames[0], Homography: append([]float64(nil), matrix...)}, {Source: frames[1], Homography: append([]float64(nil), matrix...)}}
	seed, err := canonical.Hash(map[string]any{"operation": binding.Operation, "reference": reference, "sources": frames, "matrices": [][]float64{matrix, matrix}, "inverse_fields": []any{nil, nil}, "height": 100, "width": 100})
	if err != nil {
		t.Fatal(err)
	}
	base, err := multiCanvasSpace(binding, 100, 100, regs)
	if err != nil || base != "multi:"+seed[:32] {
		t.Fatalf("worker seed: %v", err)
	}
	regs[0].Homography[2] = 1
	changed, err := multiCanvasSpace(binding, 100, 100, regs)
	if err != nil || changed == base {
		t.Fatal("nonreference matrix omitted")
	}
	regs[0].Homography[2] = 0
	regs[0].InverseField = &v2input.Ref{ArtifactID: uuid.New(), SHA256: frames[0].Source.SHA256}
	changed, err = multiCanvasSpace(binding, 100, 100, regs)
	if err != nil || changed == base {
		t.Fatal("field omitted")
	}
	regs[0].Homography[8] = 2
	if _, err := multiCanvasSpace(binding, 100, 100, regs); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("unnormalized matrix accepted")
	}
}
func TestMultiProjectionCanvasMatchesWorkerSeed(t *testing.T) {
	_, _, _, raw := originalInventoryFixture(t, "optical.panorama")
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	field := v2input.Ref{ArtifactID: uuid.New(), SHA256: image.Canvas.SourceSHA256}
	sha, err := canonical.Hash(map[string]any{"canvas": image.Canvas, "field": field})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := multiProjectionCanvas(image.Canvas, field)
	if err != nil {
		t.Fatal(err)
	}
	expected := image.Canvas
	expected.SpaceID = "cylindrical:" + sha[:32]
	if projection != expected {
		t.Fatal("projection identity mismatch")
	}
}
