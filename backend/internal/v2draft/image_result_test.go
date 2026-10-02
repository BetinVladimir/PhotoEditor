package v2draft

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func imageResultFixture() (map[string]any, v2input.Ref) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	return map[string]any{"schema_version": 1, "working_image": ref(), "changed_mask": ref(), "report": ref(), "generated_mask": nil}, ref()
}

func TestImageResultChildrenTypedRequiredNullable(t *testing.T) {
	fields, parent := imageResultFixture()
	raw, _ := canonical.Marshal(fields)
	children, err := imageResultRefs(raw, "result", parent)
	if err != nil || len(children) != 3 {
		t.Fatal(children, err)
	}
	want := []string{"WorkingImage", "MaskRef", "ImageReport"}
	for i, child := range children {
		if child.Contract != want[i] || child.ParentRef != parent {
			t.Fatal(child)
		}
	}
	fields["generated_mask"] = fields["changed_mask"]
	raw, _ = canonical.Marshal(fields)
	children, err = imageResultRefs(raw, "result", parent)
	if err != nil || len(children) != 4 || children[3].Contract != "MaskRef" || children[3].Path != "result.generated_mask" {
		t.Fatal(children, err)
	}
}

func TestImageResultRejectsMissingUnknownNullAndDuplicateFields(t *testing.T) {
	cases := []func(map[string]any){func(f map[string]any) { delete(f, "generated_mask") }, func(f map[string]any) { f["unknown"] = 1 }, func(f map[string]any) { f["working_image"] = nil }, func(f map[string]any) { f["schema_version"] = 2 }, func(f map[string]any) {
		f["report"] = map[string]any{"artifact_id": uuid.Nil, "sha256": strings.Repeat("a", 64)}
	}}
	for i, mutate := range cases {
		f, parent := imageResultFixture()
		mutate(f)
		raw, _ := canonical.Marshal(f)
		if _, err := imageResultRefs(raw, "result", parent); err == nil {
			t.Fatalf("accepted %d", i)
		}
	}
	f, parent := imageResultFixture()
	raw, _ := json.Marshal(f)
	raw = append([]byte(`{"schema_version":1,`), raw[1:]...)
	if _, err := imageResultRefs(raw, "result", parent); err == nil {
		t.Fatal("duplicate key")
	}
}
