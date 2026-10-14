package interchange

import (
	"context"
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
