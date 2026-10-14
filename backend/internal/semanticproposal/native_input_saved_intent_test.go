package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestNativeSavedIntentAndOriginalLoaderRefuseBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	got, err := reader.readNativeInputSavedPublishedIntent(ctx, auth.Identity{}, Request{}, 1<<20)
	if !errors.Is(err, context.Canceled) || got.Ref.ArtifactID != uuid.Nil {
		t.Fatal("cancelled intent read exposed publication", err)
	}
	got, err = reader.readNativeInputSavedPublishedIntent(context.Background(), auth.Identity{}, Request{}, 0)
	if !errors.Is(err, v2input.ErrInvalid) || got.Ref.ArtifactID != uuid.Nil {
		t.Fatal("missing budget reached IO", err)
	}
	p := TransferDraftPreparer{}
	for _, testCtx := range []context.Context{ctx, context.Background()} {
		ticket, err := p.loadOriginalAndResumeNativeInputTransfer(testCtx, nil, auth.Identity{}, uuid.Nil, "", nil, nil, nil, nil, nil)
		if err == nil || ticket.RunID != uuid.Nil {
			t.Fatal("incomplete original loader exposed ticket")
		}
		if testCtx == ctx && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}
