package delivery

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestDelegatedTicketRecoveryRequiresCurrentParent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := RecoverDelegatedSignerTicket(ctx, nil,
		ExpansionLease{}, Slot{}, SignerDelegationScope{})
	if !errors.Is(err, context.Canceled) { t.Fatalf("cancellation lost: %v", err) }
	_, _, err = RecoverDelegatedSignerTicket(context.Background(), nil,
		ExpansionLease{}, Slot{}, SignerDelegationScope{})
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatalf("missing parent accepted: %v", err) }
}
