package run

import (
	"github.com/google/uuid"
	"testing"
)

func TestApplyItemMaskRequestNormalizationPreservesSourceIDs(t *testing.T) {
	req := ApplyRequest{SourceRevision: 1, Scope: "all", ItemMaskOverrides: []ApplyItemMaskOverride{{ItemID: uuid.New(), OverrideID: uuid.New()}, {ItemID: uuid.New(), OverrideID: uuid.New()}}}
	normalized, _, err := normalizeApply(req)
	if err != nil || len(normalized.ItemMaskOverrides) != 2 {
		t.Fatal("per-source request refused", err)
	}
	same := req.ItemMaskOverrides[0]
	req.ItemMaskOverrides = []ApplyItemMaskOverride{same, same}
	if _, _, err = normalizeApply(req); err == nil {
		t.Fatal("duplicate item accepted")
	}
	req.ItemMaskOverrides = []ApplyItemMaskOverride{same}
	id := uuid.New()
	req.OverridesID = &id
	if _, _, err = normalizeApply(req); err == nil {
		t.Fatal("global and per-source override ambiguity accepted")
	}
}
