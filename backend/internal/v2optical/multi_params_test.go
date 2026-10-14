package v2optical

import (
	"bytes"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func multiFrames(n int) []v2input.SessionVersionBinding {
	frames := make([]v2input.SessionVersionBinding, n)
	for i := range frames {
		frames[i] = v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	}
	return frames
}
func multiCalibration() v2input.Ref {
	return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
}
func TestMultiParamsCapsAndExactOrder(t *testing.T) {
	p := PanoramaParams{SchemaVersion: 1, Frames: multiFrames(20), ReferenceOrdinal: 19, MaxPixels: 200000000, Calibration: multiCalibration(), Projection: "cylindrical"}
	raw, sha, err := p.Compile()
	if err != nil || len(sha) != 64 {
		t.Fatal(err)
	}
	parsed, err := ParsePanoramaParams(raw)
	if err != nil || parsed.Frames[19] != p.Frames[19] {
		t.Fatal(err)
	}
	p.Frames = append(p.Frames, multiFrames(1)...)
	if _, _, err = p.Compile(); err == nil {
		t.Fatal("panorama21 accepted")
	}
	f := FocusStackParams{SchemaVersion: 1, Frames: multiFrames(30), ReferenceOrdinal: 29, MaxPixels: 10000, Calibration: multiCalibration(), MotionPolicy: "reject"}
	raw, _, err = f.Compile()
	if err != nil {
		t.Fatal(err)
	}
	parsedF, err := ParseFocusStackParams(raw)
	if err != nil || parsedF.Frames[29] != f.Frames[29] {
		t.Fatal(err)
	}
	f.Frames[0], f.Frames[1] = f.Frames[1], f.Frames[0]
	reordered, _, err := f.Compile()
	if err != nil || bytes.Equal(raw, reordered) {
		t.Fatal("frame order lost")
	}
	f.Frames = append(f.Frames, multiFrames(1)...)
	if _, _, err = f.Compile(); err == nil {
		t.Fatal("focus31 accepted")
	}
}
func TestMultiParamsRejectScopeAndNoncanonical(t *testing.T) {
	base := FocusStackParams{SchemaVersion: 1, Frames: multiFrames(2), ReferenceOrdinal: 0, MaxPixels: 10000, Calibration: multiCalibration(), MotionPolicy: "review"}
	for _, name := range []string{"duplicate", "calibration_alias", "reference", "pixels", "policy", "source", "canvas"} {
		t.Run(name, func(t *testing.T) {
			p := base
			p.Frames = append([]v2input.SessionVersionBinding(nil), base.Frames...)
			switch name {
			case "duplicate":
				p.Frames[1].AssetVersionID = p.Frames[0].AssetVersionID
			case "calibration_alias":
				p.Calibration = p.Frames[0].Source
			case "reference":
				p.ReferenceOrdinal = 2
			case "pixels":
				p.MaxPixels = 0
			case "policy":
				p.MotionPolicy = "ignore"
			case "source":
				p.Frames[0].Source.ArtifactID = uuid.Nil
			case "canvas":
				p.Frames[0].CanvasHash = "bad"
			}
			if _, _, err := p.Compile(); err == nil {
				t.Fatal("invalid scope accepted")
			}
		})
	}
	raw, _, _ := base.Compile()
	if _, err := ParseFocusStackParams(append([]byte(" "), raw...)); err == nil {
		t.Fatal("noncanonical accepted")
	}
	unknown := append(append([]byte{}, raw[:len(raw)-1]...), []byte(",\"unknown\":true}")...)
	if _, err := ParseFocusStackParams(unknown); err == nil {
		t.Fatal("unknown key accepted")
	}
}
