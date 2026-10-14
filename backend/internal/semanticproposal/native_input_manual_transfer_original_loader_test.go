package semanticproposal

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestOriginalManualTransferLoaderRefusesIncompleteDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := TransferDraftPreparer{}
	for _, current := range []context.Context{ctx, context.Background()} {
		ticket, err := p.loadOriginalAndResumeNativeInputManualTransfer(current, nil, auth.Identity{}, uuid.Nil, "", nil)
		if err == nil || ticket != (v2input.JobTicket{}) {
			t.Fatal("incomplete original loader exposed ticket", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
		ticket, err = p.restoreAndResumeNativeInputManualTransfer(current, nil, auth.Identity{}, uuid.Nil, "", nil, PublishedIntent{}, v2input.Ref{}, nil)
		if err == nil || ticket != (v2input.JobTicket{}) {
			t.Fatal("incomplete original recovery exposed ticket", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
