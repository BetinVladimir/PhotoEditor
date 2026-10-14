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
		"missing no_faces": func(m map[string]any) { m["warnings"] = []string{} },
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

func TestProposalReportMultipleObjectsInLandmarkCollection(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var d proposalReportDocument
	if err := decodeExact(raw, &d); err != nil {
		t.Fatal("current Python report schema", err)
	}
	source := DefectSource{Image: d.Params.Image, Canvas: d.Protection.Canvas, TransformChain: d.Protection.TransformChain}
	if _, err := ParseProposalProtection(raw, d.Proposal, source); err != nil {
		t.Fatal("shared landmarks collection rejected", err)
	}
	original := d.Params.Faces[0].Skin.Canvas
	d.Params.Faces[0].Skin.Canvas.Width--
	wrongGeometry, _ := canonical.Marshal(d)
	if _, err := ParseProposalProtection(wrongGeometry, d.Proposal, source); err == nil {
		t.Fatal("foreign face mask canvas accepted")
	}
	d.Params.Faces[0].Skin.Canvas = original
	originalObject := d.Params.Faces[0].Skin.ObjectID
	d.Params.Faces[0].Skin.ObjectID = d.Params.Faces[1].ObjectID
	wrongObject, _ := canonical.Marshal(d)
	if _, err := ParseProposalProtection(wrongObject, d.Proposal, source); err == nil {
		t.Fatal("foreign face mask object accepted")
	}
	d.Params.Faces[0].Skin.ObjectID = originalObject
	d.Params.Faces[1].ObjectID = d.Params.Faces[0].ObjectID
	duplicate, _ := canonical.Marshal(d)
	if _, err := ParseProposalProtection(duplicate, d.Proposal, source); err == nil {
		t.Fatal("duplicate face object accepted")
	}
}

func TestProposalReportFaceEvidenceAndWarningCoherence(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var original proposalReportDocument
	if err := decodeExact(raw, &original); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: original.Params.Image, Canvas: original.Protection.Canvas, TransformChain: original.Protection.TransformChain}
	tests := map[string]func(*proposalReportDocument){
		"missing face":            func(d *proposalReportDocument) { d.Faces = d.Faces[:1] },
		"extra face":              func(d *proposalReportDocument) { d.Faces = append(d.Faces, d.Faces[0]) },
		"duplicate report object": func(d *proposalReportDocument) { d.Faces[1].ObjectID = d.Faces[0].ObjectID },
		"foreign report object":   func(d *proposalReportDocument) { d.Faces[1].ObjectID = d.LabelPNG.ID },
		"duplicate declared object across collections": func(d *proposalReportDocument) {
			d.Params.Faces[1].Landmarks.ID = d.Proposal.ID
			d.Params.Faces[1].ObjectID = d.Params.Faces[0].ObjectID
			d.Params.Faces[1].Skin.ObjectID = d.Params.Faces[0].ObjectID
		},
		"false no_faces":  func(d *proposalReportDocument) { d.Warnings = []string{"no_faces"} },
		"false skipped":   func(d *proposalReportDocument) { d.Warnings = []string{"face_skipped"} },
		"missing skipped": func(d *proposalReportDocument) { reason := "low_confidence"; d.Faces[0].Reason = &reason },
		"false overlap":   func(d *proposalReportDocument) { d.Warnings = []string{"overlapping_candidates"} },
		"missing overlap": func(d *proposalReportDocument) { d.Faces[0].Overlapping = 1 },
		"duplicate warning": func(d *proposalReportDocument) {
			reason := "low_confidence"
			d.Faces[0].Reason = &reason
			d.Warnings = []string{"face_skipped", "face_skipped"}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			var d proposalReportDocument
			if decodeExact(raw, &d) != nil {
				t.Fatal("fixture")
			}
			change(&d)
			body, err := canonical.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseProposalProtection(body, d.Proposal, source); err == nil {
				t.Fatal("incoherent evidence accepted")
			}
		})
	}
	var d proposalReportDocument
	if decodeExact(raw, &d) != nil {
		t.Fatal("fixture")
	}
	reason := "low_confidence"
	d.Faces[0].Reason = &reason
	d.Faces[1].Overlapping = 1
	d.Warnings = []string{"face_skipped", "overlapping_candidates"}
	body, _ := canonical.Marshal(d)
	if _, err := ParseProposalProtection(body, d.Proposal, source); err != nil {
		t.Fatal("valid warnings", err)
	}
	// Legacy standalone Face68 refs resolve object IDs through separate documents;
	// this parser can prove cardinality/uniqueness but cannot resolve those refs.
	d.Params.Faces[0].ObjectID = nil
	d.Params.Faces[1].ObjectID = nil
	d.Params.Faces[1].Landmarks = d.Proposal
	body, _ = canonical.Marshal(d)
	if _, err := ParseProposalProtection(body, d.Proposal, source); err != nil {
		t.Fatal("legacy standalone bindings", err)
	}
}
