package v2portrait

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRelightCandidatePageBoundsAndOrdering(t *testing.T) {
	first := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	second := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	query := RelightCandidateQuery{Kind: "depth"}
	item := func(id uuid.UUID) RelightCandidate {
		return RelightCandidate{Ref: v2input.Ref{ArtifactID: id, SHA256: strings.Repeat("a", 64)}, Kind: "depth"}
	}
	good := RelightCandidatePage{Items: []RelightCandidate{item(first), item(second)}}
	raw, _ := canonical.Marshal(good)
	if _, err := decodeRelightCandidatePage(raw, query); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"kind", "duplicate", "reverse", "digest", "cursor", "null_items"} {
		changed := RelightCandidatePage{Items: append([]RelightCandidate(nil), good.Items...)}
		switch fault {
		case "kind":
			changed.Items[0].Kind = "person"
		case "duplicate":
			changed.Items[1] = changed.Items[0]
		case "reverse":
			changed.Items[0], changed.Items[1] = changed.Items[1], changed.Items[0]
		case "digest":
			changed.Items[0].Ref.SHA256 = "invalid"
		case "cursor":
			changed.Next = &first
		case "null_items":
			changed.Items = nil
		}
		bad, _ := canonical.Marshal(changed)
		if _, err := decodeRelightCandidatePage(bad, query); err == nil {
			t.Fatal("accepted", fault)
		}
	}
	empty, _ := canonical.Marshal(RelightCandidatePage{Items: []RelightCandidate{}, Next: &second})
	query.After = &first
	if _, err := decodeRelightCandidatePage(empty, query); err != nil {
		t.Fatal("filtered empty page must advance", err)
	}
	query.After = &second
	if _, err := decodeRelightCandidatePage(empty, query); err == nil {
		t.Fatal("nonadvancing cursor")
	}
}
func TestRelightCandidateQueryRejectsIncompleteBinding(t *testing.T) {
	if err := (RelightCandidateQuery{Kind: "depth"}).validate(); err == nil {
		t.Fatal("missing binding accepted")
	}
}

func TestRelightCandidateQueryStrictEnvelope(t *testing.T) {
	query := RelightCandidateQuery{Snapshot: uuid.New(), Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}, Kind: "depth"}
	raw, _ := canonical.Marshal(query)
	if _, err := DecodeRelightCandidateQuery(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{[]byte("null"), []byte(`{"snapshot_id":"` + query.Snapshot.String() + `"}`), append(append([]byte(nil), raw...), []byte("{}")...)} {
		if _, err := DecodeRelightCandidateQuery(bad); err == nil {
			t.Fatal("malformed query accepted")
		}
	}
}
