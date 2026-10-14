package v2optical

import (
	"bytes"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func perspectiveIntent() PerspectiveParams {
	return PerspectiveParams{SchemaVersion: 1, Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}, Strength: 1, Mode: "manual", Corners: []PerspectivePoint{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, MaxCropFraction: .5}
}
func TestPerspectiveOriginalParams(t *testing.T) {
	p := perspectiveIntent()
	raw, _, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePerspectiveParams(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(append([]byte{}, raw...), 32), bytes.Replace(raw, []byte(`"strength":1`), []byte(`"strength":true`), 1), bytes.Replace(raw, []byte(`"lines":null,`), nil, 1), bytes.Replace(raw, []byte(`"mode":"manual"`), []byte(`"mode":"manual","unknown":1`), 1)} {
		if _, err := ParsePerspectiveParams(bad); err == nil {
			t.Fatal("accepted malformed intent")
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), -.1, 1.1} {
		p := perspectiveIntent()
		p.Strength = value
		if _, _, err := p.Compile(); err == nil {
			t.Fatal("invalid strength")
		}
	}
	p.Mode = "auto"
	if _, _, err := p.Compile(); err == nil {
		t.Fatal("ambiguous mode accepted")
	}
	p.Corners = nil
	p.Lines = &v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	if _, _, err := p.Compile(); err != nil {
		t.Fatal(err)
	}
}
func TestPerspectiveResourcesAndCapacity(t *testing.T) {
	required, err := PerspectiveResources(100, 80, 90, 70)
	if err != nil {
		t.Fatal(err)
	}
	if required.PixelCount != 6300 || required.RAMBytes != 32*8000+20*6300+512*1024*1024 || required.VRAMBytes != 0 {
		t.Fatal(required)
	}
	if ValidatePerspectiveCapacity(required, required) != nil {
		t.Fatal("valid capacity")
	}
	bad := required
	bad.ScratchBytes--
	if ValidatePerspectiveCapacity(required, bad) == nil {
		t.Fatal("under reserved")
	}
	for _, dims := range [][4]int64{{0, 1, 1, 1}, {200000001, 1, 1, 1}, {1, 1, 200000000, 2}} {
		if _, err := PerspectiveResources(dims[0], dims[1], dims[2], dims[3]); err == nil {
			t.Fatal(dims)
		}
	}
}

func TestPerspectiveLineFamilies(t *testing.T) {
	p := PerspectiveLineSegments{SchemaVersion: 1, Lines: []PerspectiveLine{
		{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}, Family: "horizontal"},
		{A: PerspectivePoint{0, .8}, B: PerspectivePoint{1, .8}, Family: "horizontal"},
		{A: PerspectivePoint{.2, 0}, B: PerspectivePoint{.2, 1}, Family: "vertical"},
		{A: PerspectivePoint{.8, 0}, B: PerspectivePoint{.8, 1}, Family: "vertical"},
	}}
	raw, _, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePerspectiveLineSegments(raw); err != nil {
		t.Fatal(err)
	}
	p.Lines[0].Family = "vertical"
	if _, _, err := p.Compile(); err == nil {
		t.Fatal("missing family")
	}
	p.Lines[0].Family = "horizontal"
	p.Lines[0].A = p.Lines[0].B
	if _, _, err := p.Compile(); err == nil {
		t.Fatal("zero line")
	}
}
