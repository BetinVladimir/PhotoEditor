package provenance

import (
	"context"
	"errors"
	"testing"
)

func TestTimestampServiceInstallationRequiresSQLAuthority(t *testing.T) {
	_, closeServices, err := InstallSigningTimestampServices(context.Background(),
		[]SigningAPITimestampDocument{{ID: "tsa"}}, nil)
	if !errors.Is(err, ErrInvalid) || closeServices == nil {
		t.Fatalf("missing SQL authority: %v", err)
	}
	closeServices()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, closeServices, err = InstallSigningTimestampServices(ctx, nil, nil)
	if !errors.Is(err, context.Canceled) || closeServices == nil {
		t.Fatalf("cancellation: %v", err)
	}
	closeServices()
}
