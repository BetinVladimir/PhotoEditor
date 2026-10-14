package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestMultiPreviewWitnessesOwnCompleteOutputCatalogue(t *testing.T) {
	for _, kind := range []string{"planar", "focus", "cylindrical"} {
		t.Run(kind, func(t *testing.T) {
			operation, mode := "optical.panorama", "valid"
			if kind == "focus" {
				operation = "optical.focus_stack"
			}
			if kind == "cylindrical" {
				mode = "cylindrical"
			}
			s, o, task := multiPipelineFixture(t, operation, mode)
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant, project := uuid.New(), uuid.New()
			content, err := LoadAdoptedMultiContent(context.Background(), s, o, who, tenant, project, task, operation, s.binding.Result, "photos")
			if err != nil {
				t.Fatal(err)
			}
			source := MultiPreviewSource{Tenant: tenant, Project: project, Producer: task, Binding: s.binding, Attempt: 1, Fence: 2, Principal: who}
			raw, err := content.PreviewWitnesses(source)
			if err != nil {
				t.Fatal(err)
			}
			var witnesses []MultiPreviewObjectWitness
			if err := json.Unmarshal(raw, &witnesses); err != nil {
				t.Fatal(err)
			}
			if len(witnesses) != len(content.cataloguePins)+1 || witnesses[0].Ref != s.binding.Result || witnesses[0].Path != "$" {
				t.Fatal("incomplete root/catalogue")
			}
			counts := map[string]int{}
			for _, w := range witnesses {
				if w.ObjectVersion == "" || w.ObjectVersion == "null" || w.SHA256 != w.Ref.SHA256 || w.Bytes < 1 {
					t.Fatal("unversioned evidence")
				}
				counts[w.Contract]++
			}
			if counts["WorkingPixels"] != 1 || counts["WorkingAlpha"] != 1 || counts["MaskPixels"] != 1 {
				t.Fatal("missing verified output arrays", counts)
			}
			if kind == "cylindrical" && counts["DenseWarpField"] != 2 {
				t.Fatal("missing projection fields", counts)
			}
			saved := string(raw)
			raw[0] = 'x'
			again, err := content.PreviewWitnesses(source)
			if err != nil || string(again) != saved {
				t.Fatal("evidence aliases caller bytes", err)
			}
			foreign := source
			foreign.Project = uuid.New()
			if _, err := content.PreviewWitnesses(foreign); err == nil {
				t.Fatal("foreign witness access")
			}
			content.cataloguePins = nil
			if _, err := content.PreviewWitnesses(source); err == nil {
				t.Fatal("missing completed capture accepted")
			}
		})
	}
	if _, err := (MultiResultContent{}).PreviewWitnesses(MultiPreviewSource{}); err == nil {
		t.Fatal("zero proof accepted")
	}
}

func TestMultiPreviewWitnessCaptureAbsentOnFailure(t *testing.T) {
	for _, kind := range []string{"shape", "cylindrical_bad_field"} {
		t.Run(kind, func(t *testing.T) {
			s, o, task := multiPipelineFixture(t, "optical.panorama", kind)
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant, project := uuid.New(), uuid.New()
			documents, err := LoadAdoptedMultiDocuments(context.Background(), s, o, who, tenant, project, task, s.binding.Operation, s.binding.Result, "photos")
			if err != nil {
				t.Fatal(err)
			}
			captured := false
			err = verifyAdoptedMultiOutputArrays(context.Background(), s, o, who, tenant, project, task, documents, "photos", func([]multiChildPin) { captured = true })
			if err == nil || captured {
				t.Fatal("partial array failure exposed proof", err)
			}
		})
	}
}
