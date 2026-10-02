package review

import (
	"encoding/json"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestProposalReportProtectionBinding(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var d proposalReportDocument
	if err = decodeExact(raw, &d); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: d.Params.Image, Canvas: d.Protection.Canvas, TransformChain: d.Protection.TransformChain}
	mask, err := ParseProposalProtection(raw, d.Proposal, source)
	if err != nil || mask.Artifact != d.Protection.Artifact {
		t.Fatalf("parse %v", err)
	}
	cases := map[string]func(map[string]any){
		"unprotected":      func(m map[string]any) { m["protection"].(map[string]any)["protected"] = false },
		"wrong image":      func(m map[string]any) { m["params"].(map[string]any)["image"] = d.LabelPNG },
		"wrong proposal":   func(m map[string]any) { m["proposal"] = d.LabelPNG },
		"geometry":         func(m map[string]any) { m["protection"].(map[string]any)["canvas"].(map[string]any)["width"] = 1 },
		"missing nullable": func(m map[string]any) { delete(m["params"].(map[string]any), "permanent") },
		"null faces":       func(m map[string]any) { m["faces"] = nil },
		"unknown warning":  func(m map[string]any) { m["warnings"] = []string{"other"} },
		"invalid diameter": func(m map[string]any) { m["params"].(map[string]any)["diameter_face_fraction"] = []float64{.012, .002} },
		"unknown field":    func(m map[string]any) { m["extra"] = true },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			json.Unmarshal(raw, &m)
			change(m)
			b, _ := canonical.Marshal(m)
			if _, err := ParseProposalProtection(b, d.Proposal, source); err == nil {
				t.Fatal("invalid report accepted")
			}
		})
	}
	source.TransformChain = []byte(`{"steps":[],"hash":"bad"}`)
	if _, err := ParseProposalProtection(raw, d.Proposal, source); err == nil {
		t.Fatal("changed source transform accepted")
	}
}
