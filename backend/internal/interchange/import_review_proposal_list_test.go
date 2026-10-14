package interchange

import (
	"context"
	"errors"
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
