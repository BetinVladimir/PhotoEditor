package provenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningRenderPixelAncestryRequiresAllStableProducers(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "self", "changed", "duplicate", "task"} {
		t.Run(mode, func(t *testing.T) {
			pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
			task := CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1"}
			digest, _ := canonical.Hash(task)
			inventory := SigningRenderCausalInventory{taskDigest: digest, Images: []v2input.Ref{pin(), pin()}, Results: []v2input.Ref{pin()}}
			if mode == "duplicate" {
				inventory.Results[0] = inventory.Images[0]
			}
			if mode == "task" {
				task.Task = uuid.New()
			}
			producer := uuid.New()
			calls := 0
			source := signingProducerFunc(func(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (uuid.UUID, error) {
				calls++
				switch mode {
				case "missing":
					return uuid.Nil, nil
				case "self":
					return task.Task, nil
				case "changed":
					if calls > 3 {
						return uuid.New(), nil
					}
				}
				return producer, nil
			})
			ancestry, err := ResolveSigningRenderPixelAncestry(context.Background(), source, auth.Identity{}, ReadySigningExport{}, task, inventory)
			if mode == "valid" {
				if err != nil || len(ancestry.inputs) != 3 || len(ancestry.parents) != 1 || ancestry.parents[0] != producer || calls != 6 {
					t.Fatal("pixel ancestry lost", err)
				}
				inventory.Images[0].SHA256 = "changed"
				if ancestry.inputs[0].SHA256 == "changed" {
					t.Fatal("ancestry aliases inventory")
				}
				return
			}
			if err == nil || ancestry.inputs != nil {
				t.Fatal("invalid pixel ancestry exposed")
			}
		})
	}
}

func TestSigningRenderPixelAncestryStopsOnCancellationAndProducerFailure(t *testing.T) {
	for _, mode := range []string{"cancelled", "cancel_during_read", "producer_error", "invalid_last_pin"} {
		t.Run(mode, func(t *testing.T) {
			task := CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1"}
			digest, _ := canonical.Hash(task)
			inventory := SigningRenderCausalInventory{taskDigest: digest, Images: []v2input.Ref{
				{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)},
				{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)},
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "invalid_last_pin" {
				inventory.Images[1].SHA256 = "bad"
			}
			failure := errors.New("producer authority unavailable")
			calls := 0
			producer := uuid.New()
			source := signingProducerFunc(func(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (uuid.UUID, error) {
				calls++
				if mode == "producer_error" {
					return uuid.Nil, failure
				}
				if mode == "cancel_during_read" {
					cancel()
				}
				return producer, nil
			})
			got, err := ResolveSigningRenderPixelAncestry(ctx, source, auth.Identity{}, ReadySigningExport{}, task, inventory)
			if err == nil || got.inputs != nil || got.parents != nil {
				t.Fatal("partial ancestry escaped", err)
			}
			switch mode {
			case "cancelled", "invalid_last_pin":
				if calls != 0 {
					t.Fatal("invalid request reached producer", calls)
				}
			case "cancel_during_read":
				if calls != 1 || !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation ignored", calls, err)
				}
			case "producer_error":
				if calls != 1 || !errors.Is(err, failure) {
					t.Fatal("producer failure hidden", calls, err)
				}
			}
		})
	}
}
