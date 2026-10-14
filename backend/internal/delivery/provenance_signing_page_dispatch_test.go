package delivery

import (
	"context"
	"errors"
	"testing"
)

func TestSigningPageDispatchRequiresScopedInstallation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ProcessCurrentSigningPage(ctx, ExpansionLeaseStore{}, nil,
		PreparePlan{}, ExpansionLease{}, 10, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled page accepted", err)
	}
	if _, err := ProcessCurrentSigningPage(context.Background(),
		ExpansionLeaseStore{}, nil, PreparePlan{}, ExpansionLease{}, 10,
		nil); err == nil {
		t.Fatal("unconfigured signing page accepted")
	}
	if _, failed, sweepComplete, err := ProcessCurrentSigningPageWithFailure(ctx,
		ExpansionLeaseStore{}, nil, PreparePlan{}, ExpansionLease{}, 10, nil);
		!errors.Is(err, context.Canceled) || failed != nil || sweepComplete {
		t.Fatal("cancelled failure-aware page accepted", err)
	}
}
