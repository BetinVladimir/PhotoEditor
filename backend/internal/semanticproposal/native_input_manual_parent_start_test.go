package semanticproposal

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestManualParentStartRefusesIncompleteChildAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		ticket, err := (TransferDraftPreparer{}).prepareAndActivateManualParentFromSubmission(current, auth.Identity{}, uuid.Nil, "", nil, nativeInputManualInitialSubmission{}, v2input.Built{})
		if err == nil || ticket != (v2input.JobTicket{}) {
			t.Fatal("incomplete child exposed parent ticket", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
