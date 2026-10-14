package delivery

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestSignedFailureStoreRejectsUnboundLease(t *testing.T) {
	store := ExpansionLeaseStore{}
	if err := store.FailSignedPackage(context.Background(), ExpansionLease{},
		PreparePlan{}, Slot{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("unbound signed failure accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.FailSignedPackage(ctx, ExpansionLease{},
		PreparePlan{}, Slot{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}
