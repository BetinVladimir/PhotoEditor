package v2render

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredSegmentCanonicalReadCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := AuthoredSegmentProducerReader{}
	if _, err := reader.Read(ctx, AuthoredInstructionSource{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("read cancellation lost: %v", err)
	}
	if _, err := reader.CommitCanonical(ctx, AuthoredInstructionSource{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("commit cancellation lost: %v", err)
	}
	if _, err := (SQLAuthoredSegmentContinuation{}).Resume(ctx, auth.Identity{}, [16]byte{}, [16]byte{}, 1, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("continuation cancellation lost: %v", err)
	}
}

func TestAuthoredSegmentCanonicalReadDoesNotInventMissingAuthority(t *testing.T) {
	reader := AuthoredSegmentProducerReader{}
	if _, err := reader.Read(context.Background(), AuthoredInstructionSource{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("unconfigured reader: %v", err)
	}
	if _, err := reader.CommitCanonical(context.Background(), AuthoredInstructionSource{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("unconfigured commit: %v", err)
	}
}
