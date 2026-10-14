package v2portrait

import (
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestOpenEyesParamsRequiresDonorAndConfirmation(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	base := func() v2input.SessionVersionBinding {
		return v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	}
	p := OpenEyesParams{SchemaVersion: 1, Base: base(), Donor: base(), Strength: 1, SourceFaceID: uuid.New(), DonorFaceID: uuid.New(), EyesMask: ref(), Protected: ref(), Confirmation: ref(), SourceFaces: ref(), DonorFaces: ref()}
	raw, sha, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := canonical.Hash(p)
	if sha != expected || len(raw) == 0 {
		t.Fatal("canonical pins lost")
	}
	for _, mutate := range []func(*OpenEyesParams){
		func(p *OpenEyesParams) { p.Donor = v2input.SessionVersionBinding{} },
		func(p *OpenEyesParams) { p.Confirmation = v2input.Ref{} },
		func(p *OpenEyesParams) { p.SourceFaceID = uuid.Nil },
		func(p *OpenEyesParams) { p.DonorFaces.SHA256 = "bad" },
		func(p *OpenEyesParams) { p.Strength = math.NaN() },
		func(p *OpenEyesParams) { p.Donor = p.Base },
		func(p *OpenEyesParams) { p.Protected = p.EyesMask },
	} {
		bad := p
		mutate(&bad)
		raw, sha, err := bad.Compile()
		if err == nil || raw != nil || sha != "" {
			t.Fatal("invalid donor operation compiled")
		}
	}
	p.Strength = 0
	if _, _, err := p.Compile(); err != nil {
		t.Fatal("zero-strength intent rejected", err)
	}
}

func TestOpenEyesWorkerParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/open-eyes-params-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Params OpenEyesParams `json:"params"`
		SHA    string         `json:"sha256"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	_, sha, err := fixture.Params.Compile()
	if err != nil || sha != fixture.SHA {
		t.Fatal("worker Params parity", err)
	}
}
