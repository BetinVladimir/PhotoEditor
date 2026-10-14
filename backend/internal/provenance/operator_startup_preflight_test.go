package provenance

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSigningStartupPreflightChecksActivePolicies(t *testing.T) {
	installation, _, now := signingRegistryFixture(t)
	operator := &signingAuthorityStub{}
	if err := preflightActiveSigningPolicies(context.Background(), installation, now, time.Second, operator); err != nil || operator.calls != 1 {
		t.Fatalf("active policy check: err=%v calls=%d", err, operator.calls)
	}
	operator.calls = 0
	if err := preflightActiveSigningPolicies(context.Background(), installation, now.Add(-2*time.Hour), time.Second, operator); err != nil || operator.calls != 0 {
		t.Fatalf("future policy checked too early: err=%v calls=%d", err, operator.calls)
	}
	if err := preflightActiveSigningPolicies(context.Background(), installation, now.Add(2*time.Hour), time.Second, operator); err != nil || operator.calls != 0 {
		t.Fatalf("expired policy checked at startup: err=%v calls=%d", err, operator.calls)
	}
	cause := errors.New("native signer unavailable")
	operator.err = cause
	if err := preflightActiveSigningPolicies(context.Background(), installation, now, time.Second, operator); !errors.Is(err, cause) {
		t.Fatalf("operator refusal lost: %v", err)
	}
}

func TestSigningStartupPreflightRejectsInvalidInputAndCancellation(t *testing.T) {
	installation, _, now := signingRegistryFixture(t)
	operator := &signingAuthorityStub{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := preflightActiveSigningPolicies(ctx, installation, now, time.Second, operator); !errors.Is(err, context.Canceled) || operator.calls != 0 {
		t.Fatalf("cancellation: %v", err)
	}
	if err := preflightActiveSigningPolicies(context.Background(), installation, now, 0, operator); err == nil {
		t.Fatal("zero timeout accepted")
	}
	installation.Policies[0].Body[0] = '!'
	if err := preflightActiveSigningPolicies(context.Background(), installation, now, time.Second, operator); err == nil || operator.calls != 0 {
		t.Fatalf("invalid policy reached live operator: %v", err)
	}
}
