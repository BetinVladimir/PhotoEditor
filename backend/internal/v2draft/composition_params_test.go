package v2draft

import (
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestCompositionParamsPreserveTypedRootsAndRejectBrokenExpression(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source, proposal, mask, parent := ref(), ref(), ref(), ref()
	canvas := strings.Repeat("b", 64)
	leaf := map[string]any{"op": "ref", "mask": mask, "canvas_hash": canvas}
	body := map[string]any{"schema_version": 1, "base": v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvas}, "source_result": nil, "proposal_result": proposal, "mask": map[string]any{"op": "invert", "operand": leaf}, "protected_mask": mask, "opacity": .5, "blend": "normal"}
	raw, _ := canonical.Marshal(body)
	children, handled, err := compositionParamRefs(raw, "Params", parent)
	if err != nil || !handled || len(children) != 4 {
		t.Fatal("typed params roots", err, handled, len(children))
	}
	documentChildren, e := DocumentChildren(raw, "Params", parent)
	if e != nil || len(documentChildren) != len(children) {
		t.Fatal("document params children", e)
	}
	for i, child := range documentChildren {
		if child.Ref != children[i].Ref || child.Contract != children[i].Contract || child.ParentRef != parent ||
			child.Path != "Document"+strings.TrimPrefix(children[i].Path, "Params") {
			t.Fatal("params occurrence or typed root lost", child)
		}
	}
	want := map[v2input.Ref]string{source: "WorkingImage", proposal: "ImageResult", mask: "MaskRef"}
	for _, child := range children {
		if child.Contract != want[child.Ref] || child.ParentRef != parent {
			t.Fatal("root contract lost", child)
		}
	}
	body["mask"] = map[string]any{"op": "ref", "mask": mask, "canvas_hash": strings.Repeat("c", 64)}
	raw, _ = canonical.Marshal(body)
	if _, handled, err := compositionParamRefs(raw, "Params", parent); !handled || !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("foreign canvas accepted", err)
	}
	var deep any = leaf
	for i := 0; i < 17; i++ {
		deep = map[string]any{"op": "invert", "operand": deep}
	}
	body["mask"] = deep
	raw, _ = canonical.Marshal(body)
	if _, _, err := compositionParamRefs(raw, "Params", parent); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("unbounded mask depth", err)
	}
	if _, handled, err := compositionParamRefs([]byte(`{"strength":0.5}`), "Params", parent); handled || err != nil {
		t.Fatal("unrelated params intercepted", err)
	}
}

func TestDocumentParamsKeepsGenericNestedOccurrences(t *testing.T) {
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	input := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	field := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	raw, err := canonical.Marshal(map[string]any{"settings": map[string]any{"inputs": []any{input}, "inverse_field": field}})
	if err != nil {
		t.Fatal(err)
	}
	children, err := DocumentChildren(raw, "Params", parent)
	if err != nil || len(children) != 2 {
		t.Fatal("nested params refs", err)
	}
	if children[0].Ref != input || children[0].Path != "Document.settings.inputs[0]" || children[0].Contract != "Artifact" ||
		children[1].Ref != field || children[1].Path != "Document.settings.inverse_field" || children[1].Contract != "DenseWarpField" {
		t.Fatal("nested occurrence lost", children)
	}
	for _, child := range children {
		if child.ParentRef != parent {
			t.Fatal("parent pin lost", child)
		}
	}
	for _, bad := range []string{`[]`, `{"proposal_result":null}`} {
		if _, err := DocumentChildren([]byte(bad), "Params", parent); err == nil {
			t.Fatal("invalid params admitted")
		}
	}
	for _, extra := range []any{nil, true, map[string]any{"value": 1}} {
		bad, err := canonical.Marshal(map[string]any{"settings": []any{map[string]any{
			"artifact_id": input.ArtifactID, "sha256": input.SHA256, "extra": extra,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DocumentChildren(bad, "Params", parent); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("malformed reference silently omitted", err)
		}
	}
}

func TestOpaqueDocumentChildrenExposeOnlyDeclaredBoundedReferences(t *testing.T) {
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	child := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	raw, err := canonical.Marshal(map[string]any{"entries": []any{child}})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := OpaqueDocumentChildren(raw, "Artifact", parent)
	if err != nil || len(refs) != 1 || refs[0].Ref != child || refs[0].ParentRef != parent || refs[0].Path != "Document.entries[0]" || refs[0].Contract != "Artifact" {
		t.Fatal(refs, err)
	}
	for _, bad := range []string{`{"broken":`, `{"entry":{"artifact_id":"bad","sha256":"bad"}}`, `{"entry":{"artifact_id":"bad","sha256":"bad","extra":true}}`} {
		if _, err := OpaqueDocumentChildren([]byte(bad), "Artifact", parent); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("malformed opaque refs accepted", err)
		}
	}
}
