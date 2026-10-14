package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

func TestNativeBootstrapRetainsCheckpointWithoutFailedReceipt(t *testing.T) {
	for _, failure := range []string{"", "prepare", "persist", "admit", "cancel_prepare", "cancel_persist", "cancel_admit"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			dependency := errors.New("bootstrap dependency failure")
			expected := v2input.JobTicket{RunID: uuid.New(), WorkflowID: uuid.New(), State: "queued"}
			type checkpoint struct{ id uuid.UUID }
			owned := checkpoint{id: uuid.New()}
			var phases []string
			plan, ticket, err := bootstrapNativePreparation(ctx,
				func() (checkpoint, error) {
					phases = append(phases, "prepare")
					if failure == "prepare" {
						return owned, dependency
					}
					if failure == "cancel_prepare" {
						cancel()
					}
					return owned, nil
				},
				func(plan checkpoint) error {
					phases = append(phases, "persist")
					if plan != owned {
						t.Fatal("lost owned plan")
					}
					if failure == "persist" {
						return dependency
					}
					if failure == "cancel_persist" {
						cancel()
					}
					return nil
				},
				func(plan checkpoint) (v2input.JobTicket, error) {
					phases = append(phases, "admit")
					if plan != owned {
						t.Fatal("substituted admission plan")
					}
					if failure == "admit" {
						return expected, dependency
					}
					if failure == "cancel_admit" {
						cancel()
					}
					return expected, nil
				})
			want := []string{"prepare", "persist", "admit"}
			if failure == "prepare" || failure == "cancel_prepare" {
				want = want[:1]
			}
			if failure == "persist" || failure == "cancel_persist" {
				want = want[:2]
			}
			if !reflect.DeepEqual(phases, want) {
				t.Fatal("unsafe phase order", phases)
			}
			if failure == "" {
				if err != nil || plan != owned || ticket != expected {
					t.Fatal("bootstrap outcome", err)
				}
				return
			}
			if err == nil || ticket != (v2input.JobTicket{}) {
				t.Fatal("failure exposed receipt", err)
			}
			if failure == "prepare" {
				if plan != (checkpoint{}) {
					t.Fatal("failed preparation exposed checkpoint")
				}
			} else if plan != owned {
				t.Fatal("recovery checkpoint lost")
			}
			if failure == "prepare" || failure == "persist" || failure == "admit" {
				if !errors.Is(err, dependency) {
					t.Fatal("lost dependency error", err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
		})
	}
}

func TestNativeBootstrapDependencyAndCancellationPreflight(t *testing.T) {
	bootstrap := nativePreparationBootstrap{}
	if _, ticket, err := bootstrap.bootstrapDetector(context.Background(), auth.Identity{}, nativePreparationAnchor{}, v2input.Ref{}, .8, 10, nativePreparationBudget{}); !errors.Is(err, v2input.ErrInvalid) || ticket != (v2input.JobTicket{}) {
		t.Fatal("missing detector dependencies", err)
	}
	if _, ticket, err := bootstrap.bootstrapPerson(context.Background(), auth.Identity{}, nativePreparationAnchor{}, v2input.Ref{}, 512, nativePreparationBudget{}); !errors.Is(err, v2input.ErrInvalid) || ticket != (v2input.JobTicket{}) {
		t.Fatal("missing person dependencies", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := bootstrap.bootstrapDetector(ctx, auth.Identity{}, nativePreparationAnchor{}, v2input.Ref{}, .8, 10, nativePreparationBudget{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation precedence", err)
	}
}
