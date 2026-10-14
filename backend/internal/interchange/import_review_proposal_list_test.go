package interchange

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestImportReviewProposalListRequiresInstalledDependencies(t *testing.T) {
	service := ImportReviewProposalListService{}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	_, err := service.List(context.Background(), who, uuid.New(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("uninstalled proposal list accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.List(ctx, who, uuid.New(), uuid.New(), uuid.New())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list: %v", err)
	}
}

func TestImportReviewProposalCursorUsesExactCompositeIdentity(t *testing.T) {
	item := ImportReviewProposalItem{EntryID: uuid.New(), TargetVersionID: uuid.New(), Kind: "defects"}
	item.Proposal.ArtifactID = uuid.New()
	entry, target, kind, proposal, err := parseImportReviewSavedCursor(importReviewProposalCursor(item))
	if err != nil || entry != item.EntryID || target != item.TargetVersionID || kind != item.Kind || proposal != item.Proposal.ArtifactID {
		t.Fatal("proposal page cursor lost exact identity", err)
	}
}

func TestImportReviewProposalListCollectsAllPagesWithoutSilentTruncation(t *testing.T) {
	items := make([]ImportReviewProposalItem, 51)
	for i := range items {
		items[i] = ImportReviewProposalItem{EntryID: uuid.New(), TargetVersionID: uuid.New(), Kind: "defects"}
		items[i].Proposal.ArtifactID = uuid.New()
	}
	sort.Slice(items, func(i, j int) bool {
		return importReviewProposalCursor(items[i]) < importReviewProposalCursor(items[j])
	})
	firstCursor := importReviewProposalCursor(items[49])
	calls := 0
	got, err := collectImportReviewProposalPages(context.Background(), func(after string) (ImportReviewProposalPage, error) {
		calls++
		switch after {
		case "":
			return ImportReviewProposalPage{Items: items[:50], NextAfter: firstCursor}, nil
		case firstCursor:
			return ImportReviewProposalPage{Items: items[50:]}, nil
		default:
			t.Fatal("unexpected cursor", after)
			return ImportReviewProposalPage{}, nil
		}
	})
	if err != nil || calls != 2 || len(got) != 51 {
		t.Fatal("complete list lost a page", calls, len(got), err)
	}
	_, err = collectImportReviewProposalPages(context.Background(), func(after string) (ImportReviewProposalPage, error) {
		if after == "" {
			return ImportReviewProposalPage{Items: items[:50], NextAfter: firstCursor}, nil
		}
		return ImportReviewProposalPage{Items: items[49:]}, nil
	})
	if err == nil {
		t.Fatal("overlapping page accepted")
	}
}
