package delivery

import (
	"context"
	"errors"
	"testing"
)

func TestDelegatedClosureRequiresInstalledPhysicalAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (DelegatedSigningClosureCoordinator{}).VerifyAndRetain(
		ctx, PreparePlan{}, ExpansionLease{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled closure accepted", err)
	}
	if _, _, err := (DelegatedSigningClosureCoordinator{}).VerifyAndRetain(
		context.Background(), PreparePlan{}, ExpansionLease{}); err == nil {
		t.Fatal("unconfigured closure accepted")
	}
}
