package provenance

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func TestSigningTimestampRouterExactAuthorityAndOwnership(t *testing.T) {
	for _, mode := range []string{"valid", "foreign_policy", "foreign_chain", "service_error", "cancel_before", "cancel_during"} {
		t.Run(mode, func(t *testing.T) {
			_, chain, policy, _ := operatorChallengeFixture(t, "ED25519")
			service := "qualified-tsa"
			policy.TimestampMode = "required"
			policy.TimestampServiceID = &service
			raw, err := canonical.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			originalChain := append([]byte(nil), chain...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			sentinel := errors.New("TSA refused")
			services := map[string]SigningTrustCheck{service: func(ctx context.Context, p SigningPolicy, c []byte) error {
				calls++
				if *p.TimestampServiceID != "qualified-tsa" {
					t.Fatal("wrong TSA")
				}
				*p.TimestampServiceID = "mutated"
				p.AllowedMIME[0] = "mutated"
				c[0] = '!'
				if mode == "service_error" {
					return sentinel
				}
				if mode == "cancel_during" {
					cancel()
				}
				return nil
			}}
			router, err := NewSigningTimestampRouter([][]byte{raw}, services)
			if err != nil {
				t.Fatal(err)
			}
			raw[0] = '!'
			services[service] = func(context.Context, SigningPolicy, []byte) error { t.Fatal("retained caller map"); return nil }
			candidate := append([]byte(nil), chain...)
			if mode == "foreign_policy" {
				policy.Revision = "2"
			}
			if mode == "foreign_chain" {
				candidate[0] = '!'
			}
			if mode == "cancel_before" {
				cancel()
			}
			err = router.Check(ctx, policy, candidate)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if mode == "service_error" && !errors.Is(err, sentinel) {
				t.Fatal("lost TSA refusal")
			}
			if mode == "cancel_during" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation")
			}
			want := 1
			if mode == "foreign_policy" || mode == "foreign_chain" || mode == "cancel_before" {
				want = 0
			}
			if calls != want {
				t.Fatalf("calls=%d want=%d", calls, want)
			}
			if *policy.TimestampServiceID != "qualified-tsa" || string(chain) != string(originalChain) {
				t.Fatal("callback mutated caller")
			}
		})
	}
}

func TestSigningTimestampRouterRejectsIncompleteInstallation(t *testing.T) {
	for _, mode := range []string{"missing", "nil", "unused", "duplicate", "off"} {
		t.Run(mode, func(t *testing.T) {
			_, _, policy, _ := operatorChallengeFixture(t, "ED25519")
			id := "tsa"
			policy.TimestampMode = "required"
			policy.TimestampServiceID = &id
			if mode == "off" {
				policy.TimestampMode = "off"
				policy.TimestampServiceID = nil
			}
			raw, err := canonical.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			bodies := [][]byte{raw}
			services := map[string]SigningTrustCheck{id: func(context.Context, SigningPolicy, []byte) error { return nil }}
			switch mode {
			case "missing":
				delete(services, id)
			case "nil":
				services[id] = nil
			case "unused":
				services["other"] = services[id]
			case "duplicate":
				bodies = append(bodies, raw)
			}
			if _, err = NewSigningTimestampRouter(bodies, services); err == nil {
				t.Fatal("invalid installation accepted")
			}
		})
	}
}
