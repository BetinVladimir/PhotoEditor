package v2draft

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestManualRasterParamsDeclareExternalVectors(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source, vectors, parent := ref(), ref(), ref()
	body := map[string]any{"image": source, "vectors": vectors, "inline_vectors": nil, "object_id": nil}
	raw, _ := canonical.Marshal(body)
	children, err := DocumentChildren(raw, "Params", parent)
	if err != nil || len(children) != 2 || children[0].Contract != "WorkingImage" || children[1].Contract != "ManualMask" || children[1].Ref != vectors || children[1].ParentRef != parent {
		t.Fatal(children, err)
	}
	body["inline_vectors"] = map[string]any{}
	raw, _ = canonical.Marshal(body)
	if _, err := DocumentChildren(raw, "Params", parent); err == nil {
		t.Fatal("two vector sources accepted")
	}
	body["inline_vectors"] = nil
	body["extra"] = true
	raw, _ = canonical.Marshal(body)
	if _, err := DocumentChildren(raw, "Params", parent); err == nil {
		t.Fatal("unknown field accepted")
	}
}
