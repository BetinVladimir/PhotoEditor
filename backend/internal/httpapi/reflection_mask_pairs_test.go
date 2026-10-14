package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestReflectionMaskPairPublicContract(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	item := v2draft.GlareMaskPair{GlassesMask: ref(), Protected: ref(), TargetPixel: ref(), ProtectedPixel: ref()}
	cursor := item.GlassesMask.ArtifactID
	page := reflectionMaskPage(v2draft.GlareMaskPairPage{Items: []v2draft.GlareMaskPair{item}, Next: &cursor})
	if len(page.Items) != 1 || page.Items[0].Mask != item.GlassesMask || page.Next == nil || *page.Next != cursor {
		t.Fatal("reflection mask pair changed")
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	row := body["items"].([]any)[0].(map[string]any)
	if row["mask"] == nil || row["glasses_mask"] != nil {
		t.Fatal("donor-only field leaked into reflection response")
	}
}
