package v2render

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredDefectTransportCancellationPrecedesPublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (SQLAuthoredDefectProposalPreparation{}).PublishTransport(ctx, auth.Identity{}, QualifiedAuthoredDefectProposalPlan{}, "", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation identity lost: %v", err)
	}
}
func TestAuthoredDefectTransportRejectsUnpreparedAuthority(t *testing.T) {
	_, err := (SQLAuthoredDefectProposalPreparation{}).PublishTransport(context.Background(), auth.Identity{}, QualifiedAuthoredDefectProposalPlan{}, "bucket", nil)
	if !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("empty proof admitted: %v", err)
	}
}
