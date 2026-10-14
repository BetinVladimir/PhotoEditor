package collection

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func TestOptionalUUIDDistinguishesMissingNullAndValue(t *testing.T) {
	type request struct {
		Cover OptionalUUID `json:"cover_item_id,omitempty"`
	}
	var missing request
	if err := json.Unmarshal([]byte(`{}`), &missing); err != nil || missing.Cover.Set {
		t.Fatalf("missing: %+v %v", missing, err)
	}
	var cleared request
	if err := json.Unmarshal([]byte(`{"cover_item_id":null}`), &cleared); err != nil || !cleared.Cover.Set || cleared.Cover.Value != nil {
		t.Fatalf("null: %+v %v", cleared, err)
	}
	id := uuid.New()
	var set request
	if err := json.Unmarshal([]byte(`{"cover_item_id":"`+id.String()+`"}`), &set); err != nil || !set.Cover.Set || set.Cover.Value == nil || *set.Cover.Value != id {
		t.Fatalf("value: %+v %v", set, err)
	}
	for _, raw := range []string{`{"cover_item_id":""}`, `{"cover_item_id":"00000000-0000-0000-0000-000000000000"}`} {
		if json.Unmarshal([]byte(raw), new(request)) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestOptionalColorLabelDistinguishesMissingClearAndPalette(t *testing.T) {
	var missing, cleared, selected ItemPatch
	if err := json.Unmarshal([]byte(`{}`), &missing); err != nil || missing.ColorLabel.Set {
		t.Fatalf("missing: %+v %v", missing.ColorLabel, err)
	}
	if err := json.Unmarshal([]byte(`{"color_label":null}`), &cleared); err != nil || !cleared.ColorLabel.Set || cleared.ColorLabel.Value != nil {
		t.Fatalf("clear: %+v %v", cleared.ColorLabel, err)
	}
	if err := json.Unmarshal([]byte(`{"color_label":"Blue"}`), &selected); err != nil || !selected.ColorLabel.Set || selected.ColorLabel.Value == nil || *selected.ColorLabel.Value != "Blue" {
		t.Fatalf("select: %+v %v", selected.ColorLabel, err)
	}
	for _, raw := range []string{`{"color_label":"blue"}`, `{"color_label":"Custom"}`, `{"color_label":""}`, `{"color_label":5}`} {
		if json.Unmarshal([]byte(raw), new(ItemPatch)) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestApplyItemModerationValidation(t *testing.T) {
	service := Service{}
	for _, request := range []ItemModeration{{ExpectedRevision: 0, Decision: "accept"}, {ExpectedRevision: 1, Decision: "approve"}, {ExpectedRevision: 1}} {
		_, err := service.ModerateApplyItem(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), request)
		if err == nil || domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatalf("accepted invalid moderation: %+v, %v", request, err)
		}
	}
}

func TestCollectionValidation(t *testing.T) {
	if !valid(" Portfolio ", "", "manual", "draft") || !valid("Portfolio", "description", "filename", "published") {
		t.Fatal("valid collection rejected")
	}
	for _, values := range [][4]string{{"", "", "manual", "draft"}, {"x", "", "random", "draft"}, {"x", "", "manual", "deleting"}} {
		if valid(values[0], values[1], values[2], values[3]) {
			t.Fatalf("accepted %#v", values)
		}
	}
}

func TestSectionAndTagValidation(t *testing.T) {
	if !validSection("Highlights", 0) || validSection("", 0) || validSection("x", -1) || validSection("x", 9007199254740992) {
		t.Fatal("section validation mismatch")
	}
	if !validTags([]string{"portrait", "favorite"}) || validTags([]string{"portrait", "portrait"}) || validTags([]string{" spaced "}) || validTags([]string{""}) {
		t.Fatal("tag validation mismatch")
	}
}

func TestPublicationIDsAreUniqueAndSorted(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	ids, ok := sortedUniqueIDs([]uuid.UUID{b, a}, 500)
	if !ok || len(ids) != 2 || string(ids[0][:]) > string(ids[1][:]) {
		t.Fatalf("not sorted: %v", ids)
	}
	for _, input := range [][]uuid.UUID{{}, {a, a}, {uuid.Nil}} {
		if _, ok = sortedUniqueIDs(input, 500); ok {
			t.Fatalf("accepted %v", input)
		}
	}
}
