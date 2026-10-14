package delivery

import (
	"context"
	"errors"
	"testing"
)

func TestDelegatedSigningSlotDispatcherRequiresInstalledAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (DelegatedSigningSlotDispatcher{}).Dispatch(ctx,
		ExpansionLease{}, Slot{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled dispatch accepted", err)
	}
	if err := (DelegatedSigningSlotDispatcher{}).Dispatch(context.Background(),
		ExpansionLease{}, Slot{}); err == nil {
		t.Fatal("unconfigured dispatch accepted")
	}
}
