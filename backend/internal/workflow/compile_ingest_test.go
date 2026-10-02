package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"reflect"
	"strings"
	"testing"
)

func ingestFixture(t *testing.T) (IngestSpec, Registry) {
	t.Helper()
	r, e := LoadRegistry()
	if e != nil {
		t.Fatal(e)
	}
	return IngestSpec{RunID: uuid.New(), Items: []IngestItem{{Ordinal: 8, Source: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1234, Suffix: ".png"}, {Ordinal: 2, Source: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Bytes: 2345, Suffix: ".nef", Analyze: true}}}, r
}
func TestCompileIngestDeterministicAndDeclaresSource(t *testing.T) {
	s, r := ingestFixture(t)
	p, e := CompileIngest(s, r)
	if e != nil || len(p.Nodes) != 6 || len(p.Graph.Edges) != 4 {
		t.Fatal(p, e)
	}
	s.Items[0], s.Items[1] = s.Items[1], s.Items[0]
	q, e := CompileIngest(s, r)
	if e != nil || !reflect.DeepEqual(p, q) {
		t.Fatal("unstable plan")
	}
	for _, node := range p.Nodes {
		var params map[string]json.RawMessage
		if json.Unmarshal(node.Params, &params) != nil {
			t.Fatal("invalid params")
		}
		var source review.ArtifactRef
		json.Unmarshal(params["source"], &source)
		if len(node.Inputs) != 1 || node.Inputs[0] != source {
			t.Fatal("nested source undeclared")
		}
		if node.Operation == "asset.decode" && string(params["tile"]) != "512" {
			t.Fatal("default tile lost")
		}
	}
	s.RunID = uuid.New()
	q, _ = CompileIngest(s, r)
	if p.Nodes[0].ID == q.Nodes[0].ID {
		t.Fatal("cross-run task collision")
	}
}
func TestCompileIngestRejectsInvalidInputs(t *testing.T) {
	for _, scenario := range []string{"duplicate", "suffix", "size", "profile", "tile", "registry"} {
		t.Run(scenario, func(t *testing.T) {
			s, r := ingestFixture(t)
			switch scenario {
			case "duplicate":
				s.Items[1].Ordinal = s.Items[0].Ordinal
			case "suffix":
				s.Items[0].Suffix = "../../image.png"
			case "size":
				s.Items[0].Bytes = 536870913
			case "profile":
				bad := "unknown"
				s.Items[0].AssumeProfile = &bad
			case "tile":
				s.Items[0].Tile = 128
			case "registry":
				delete(r, "asset.decode")
			}
			if _, e := CompileIngest(s, r); e == nil {
				t.Fatal("invalid spec accepted")
			}
		})
	}
}
