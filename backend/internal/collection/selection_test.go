package collection

import (
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeOwnerSelection(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	request, revision, err := normalizeSelection(SelectionRequest{CollectionRevision: "7", Scope: " SELECTED ", ItemIDs: []uuid.UUID{b, a}})
	if err != nil || revision != 7 || request.Scope != "selected" || len(request.ItemIDs) != 2 || string(request.ItemIDs[0][:]) > string(request.ItemIDs[1][:]) {
		t.Fatalf("selected: %#v %d %v", request, revision, err)
	}
	section := uuid.New()
	rating := 4
	flag := " PICK "
	tag := "portrait"
	request, _, err = normalizeSelection(SelectionRequest{CollectionRevision: "8", Scope: "filter", Filter: &SelectionFilter{SectionID: &section, Rating: &rating, Flag: &flag, Tag: &tag}})
	if err != nil || request.Filter == nil || *request.Filter.Flag != "pick" {
		t.Fatalf("filter: %#v %v", request, err)
	}
}

func TestNormalizeOwnerSelectionRejectsAmbiguity(t *testing.T) {
	id := uuid.New()
	cases := []SelectionRequest{
		{CollectionRevision: "0", Scope: "all"},
		{CollectionRevision: "1", Scope: "selected"},
		{CollectionRevision: "1", Scope: "selected", ItemIDs: []uuid.UUID{id, id}},
		{CollectionRevision: "1", Scope: "all", ItemIDs: []uuid.UUID{id}},
		{CollectionRevision: "1", Scope: "filter", Filter: &SelectionFilter{}},
	}
	for _, request := range cases {
		if _, _, err := normalizeSelection(request); err == nil {
			t.Fatalf("accepted %#v", request)
		}
	}
}
