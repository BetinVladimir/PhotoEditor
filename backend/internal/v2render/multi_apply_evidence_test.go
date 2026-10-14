package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestMultiApplyEvidenceCompleteOwnedAndScoped(t *testing.T) {
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
			originals, err := content.originalInventory(s.binding, task)
			if err != nil || len(originals) < 2 {
				t.Fatal("missing original inventory", err)
			}
			savedOriginals := string(originals)
			originals[0] = 'x'
			again, err := content.originalInventory(s.binding, task)
			if err != nil || string(again) != savedOriginals {
				t.Fatal("original inventory aliases caller", err)
			}
			if _, err := content.originalInventory(s.binding, uuid.New()); err == nil {
				t.Fatal("foreign original producer accepted")
			}
			root, image, raw, err := content.applyEvidence(tenant, project, task, s.binding)
			if err != nil {
				t.Fatal(err)
			}
			var witnesses []MultiPreviewObjectWitness
			if json.Unmarshal(raw, &witnesses) != nil || len(witnesses) != len(content.cataloguePins)+1 {
				t.Fatal("incomplete witnesses")
			}
			if witnesses[0].Ref != s.binding.Result || len(root) < 2 || len(image) < 2 {
				t.Fatal("missing root/image")
			}
			savedRoot, savedImage, savedWitness := string(root), string(image), string(raw)
			root[0] = 'x'
			image[0] = 'x'
			raw[0] = 'x'
			r, i, w, err := content.applyEvidence(tenant, project, task, s.binding)
			if err != nil || string(r) != savedRoot || string(i) != savedImage || string(w) != savedWitness {
				t.Fatal("aliased evidence", err)
			}
			if _, _, _, err := content.applyEvidence(tenant, uuid.New(), task, s.binding); err == nil {
				t.Fatal("foreign project accepted")
			}
			if _, _, _, err := content.applyEvidence(tenant, project, uuid.New(), s.binding); err == nil {
				t.Fatal("foreign producer accepted")
			}
			content.cataloguePins = nil
			if _, _, _, err := content.applyEvidence(tenant, project, task, s.binding); err == nil {
				t.Fatal("missing output capture accepted")
			}
		})
	}
	if _, _, _, err := (MultiResultContent{}).applyEvidence(uuid.New(), uuid.New(), uuid.New(), AdoptedMultiBinding{}); err == nil {
		t.Fatal("zero proof accepted")
	}
}
