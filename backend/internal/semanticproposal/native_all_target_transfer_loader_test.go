package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeAllTargetLoaderRefusesCancelledOrUnconfiguredEntry(t *testing.T) {
	p := TransferDraftPreparer{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name string
		ctx  context.Context
		want error
	}{{"cancelled", ctx, context.Canceled}, {"unconfigured", context.Background(), v2input.ErrInvalid}} {
		t.Run(test.name, func(t *testing.T) {
			ticket, err := p.loadAndResumeNativeAllTargetTransfer(test.ctx, auth.Identity{}, uuid.Nil, "", nil, TransferColorProfile{})
			if !errors.Is(err, test.want) || ticket != (v2input.JobTicket{}) {
				t.Fatalf("unexpected entry result: %v %v", ticket, err)
			}
			plan, capacity, err := p.restoreNativeAllTargetTransferPlan(test.ctx, auth.Identity{}, detectorDraftPlan{}, nativeInputTransferReferenceDocuments{}, TransferColorProfile{}, nil)
			if !errors.Is(err, test.want) || plan.core.run != uuid.Nil || capacity.PersonProtection != nil {
				t.Fatalf("unexpected restore authority: %v", err)
			}
		})
	}
}
