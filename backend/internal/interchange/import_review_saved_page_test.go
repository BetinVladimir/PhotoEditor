package interchange

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestImportReviewSavedCursorIsExactAndScoped(t *testing.T) {
	row := ImportReviewDecisionStatus{EntryID: uuid.New(), TargetVersionID: uuid.New(), Kind: "dust"}
	row.Proposal.ArtifactID = uuid.New()
	cursor := importReviewSavedCursor(row)
	entry, target, kind, proposal, err := parseImportReviewSavedCursor(cursor)
	if err != nil || entry != row.EntryID || target != row.TargetVersionID || kind != row.Kind || proposal != row.Proposal.ArtifactID {
		t.Fatal("saved review cursor did not round-trip", err)
	}
	for _, invalid := range []string{".", row.EntryID.String() + "." + row.TargetVersionID.String() + ".other." + row.Proposal.ArtifactID.String(),
		uuid.Nil.String() + "." + row.TargetVersionID.String() + ".dust." + row.Proposal.ArtifactID.String(), cursor + ".extra"} {
		if _, _, _, _, err := parseImportReviewSavedCursor(invalid); err == nil {
			t.Fatal("invalid saved review cursor accepted", invalid)
		}
	}
	if _, err := (ImportReviewDecisionService{}).ListSavedPage(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), "invalid"); err == nil {
		t.Fatal("malformed cursor reached database dependency")
	}
}

func TestImportReviewSavedListCollectsAllPagesAndRejectsOverlap(t *testing.T) {
	items := make([]ImportReviewDecisionStatus, 501)
	for i := range items {
		items[i] = ImportReviewDecisionStatus{EntryID: uuid.New(), TargetVersionID: uuid.New(), Kind: "dust"}
		items[i].Proposal.ArtifactID = uuid.New()
	}
	sort.Slice(items, func(i, j int) bool { return importReviewSavedCursor(items[i]) < importReviewSavedCursor(items[j]) })
	firstCursor := importReviewSavedCursor(items[499])
	calls := 0
	got, err := collectImportReviewSavedPages(context.Background(), func(after string) (ImportReviewSavedPage, error) {
		calls++
		if after == "" {
			return ImportReviewSavedPage{Items: items[:500], NextAfter: firstCursor}, nil
		}
		if after != firstCursor {
			t.Fatal("unexpected saved cursor", after)
		}
		return ImportReviewSavedPage{Items: items[500:]}, nil
	})
	if err != nil || calls != 2 || len(got) != 501 {
		t.Fatal("saved decisions were truncated", calls, len(got), err)
	}
	_, err = collectImportReviewSavedPages(context.Background(), func(after string) (ImportReviewSavedPage, error) {
		if after == "" {
			return ImportReviewSavedPage{Items: items[:500], NextAfter: firstCursor}, nil
		}
		return ImportReviewSavedPage{Items: items[499:]}, nil
	})
	if err == nil {
		t.Fatal("overlapping saved page accepted")
	}
}
