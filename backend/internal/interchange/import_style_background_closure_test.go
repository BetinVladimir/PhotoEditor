package interchange

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestImportedStylePortableBackgroundClosureRequiresExactTypedChild(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "foreign_id", "role", "duplicate", "hash", "non_uuid", "nonasset"} {
		t.Run(mode, func(t *testing.T) {
			id := uuid.New()
			portable := id.String()
			backgroundMode := "asset"
			if mode == "non_uuid" {
				portable = "original-portable-id"
			}
			if mode == "nonasset" {
				backgroundMode = "color"
			}
			raw, err := canonical.Marshal(map[string]any{"extensions": map[string]any{"background": map[string]any{"mode": backgroundMode, "asset_id": portable}}})
			if err != nil {
				t.Fatal(err)
			}
			child := v2input.Entry{Contract: "StyleBackgroundAsset", Ref: v2input.Ref{ArtifactID: id, SHA256: rawSHA([]byte("background"))}}
			children := []v2input.Entry{child}
			switch mode {
			case "missing":
				children = nil
			case "foreign_id":
				children[0].Ref.ArtifactID = uuid.New()
			case "role":
				children[0].Contract = "Artifact"
			case "duplicate":
				children = append(children, child)
			case "hash":
				children[0].Ref.SHA256 = "invalid"
			case "nonasset":
				children = nil
			}
			refs, err := importClosureDocumentRefs(raw, "StyleV2", children)
			if mode == "valid" {
				if err != nil || len(refs) != 1 || refs[id] != child.Ref {
					t.Fatal("portable dependency omitted", err)
				}
			} else if mode == "nonasset" {
				if err != nil || len(refs) != 0 {
					t.Fatal("nonasset acquired dependency", err)
				}
			} else if err == nil {
				t.Fatal("invalid portable dependency accepted")
			}
		})
	}
}
func TestImportedOverridePortableBackgroundCannotBypassClosure(t *testing.T) {
	for _, mode := range []string{"asset", "keep"} {
		raw, err := canonical.Marshal(map[string]any{"modules": map[string]any{"extensions": map[string]any{"background": map[string]any{"mode": mode, "asset_id": uuid.New().String()}}}})
		if err != nil {
			t.Fatal(err)
		}
		refs, err := importClosureDocumentRefs(raw, "Override", nil)
		if mode == "asset" {
			if err == nil || refs != nil {
				t.Fatal("unregistered Override portable asset escaped closure")
			}
		} else if err != nil || len(refs) != 0 {
			t.Fatal("non-asset Override was rejected", err)
		}
	}
}
