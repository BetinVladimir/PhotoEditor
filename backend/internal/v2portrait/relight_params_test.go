package v2portrait

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestRelightLightSchemaBounds(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/implementation-v2/contracts/schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Items struct {
					Properties map[string]struct{ Minimum, Maximum float64 }
				}
			}
		} `json:"$defs"`
	}
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: ref.SHA256}
	makeParams := func() RelightParams {
		return RelightParams{SchemaVersion: 1, Base: base, Depth: ref, Subject: ref, Protected: ref, Calibration: ref, Lights: []RelightLight{{Radius: .5}}}
	}
	for _, field := range []string{"x", "y", "radius", "ev"} {
		bounds, ok := spec.Defs["ImageRelightParams"].Properties["lights"].Items.Properties[field]
		if !ok {
			t.Fatal("missing schema bounds", field)
		}
		for _, value := range []float64{bounds.Minimum, bounds.Maximum, bounds.Minimum - .01, bounds.Maximum + .01, math.NaN(), math.Inf(1), math.Inf(-1)} {
			p := makeParams()
			switch field {
			case "x":
				p.Lights[0].X = value
			case "y":
				p.Lights[0].Y = value
			case "radius":
				p.Lights[0].Radius = value
			case "ev":
				p.Lights[0].EV = value
			}
			body, sha, err := p.Compile()
			valid := finiteRange(value, bounds.Minimum, bounds.Maximum)
			if (err == nil) != valid || (!valid && (body != nil || sha != "")) {
				t.Fatalf("%s=%v accepted incorrectly: %v", field, value, err)
			}
		}
	}
	for _, count := range []int{0, 1, 4, 5} {
		p := makeParams()
		p.Lights = make([]RelightLight, count)
		for i := range p.Lights {
			p.Lights[i].Radius = .05
		}
		_, _, err := p.Compile()
		if (err == nil) != (count >= 1 && count <= 4) {
			t.Fatal("light count", count, err)
		}
	}
	for _, mutate := range []func(*RelightParams){
		func(p *RelightParams) { p.SchemaVersion = 2 }, func(p *RelightParams) { p.Strength = math.NaN() },
		func(p *RelightParams) { p.Depth.ArtifactID = uuid.Nil }, func(p *RelightParams) { p.Calibration.SHA256 = "invalid" },
		func(p *RelightParams) { p.Base.CanvasHash = "invalid" }, func(p *RelightParams) { p.Subject.ArtifactID = uuid.Nil },
	} {
		p := makeParams()
		mutate(&p)
		if body, sha, err := p.Compile(); err == nil || body != nil || sha != "" {
			t.Fatal("invalid binding produced Params")
		}
	}
}
