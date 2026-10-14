package workflow

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

func TestVerifyAndPublishSignedAssemblyRejectsIncompleteInstallation(t *testing.T) {
	p := DeliveryAssemblyPipeline{}
	state, publication, evidence, err := p.VerifyAndPublishSignedAssembly(
		context.Background(), delivery.ExpansionLease{}, delivery.PreparePlan{},
		delivery.DelegatedSigningClosureCoordinator{})
	if state != "" || !errors.Is(err, v2input.ErrInvalid) ||
		len(publication.ShardJCS) != 0 || len(evidence.Receipts) != 0 {
		t.Fatalf("incomplete verifier installation must publish nothing: state=%q err=%v", state, err)
	}
}

func TestVerifyAndPublishSignedAssemblyHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state, _, _, err := (DeliveryAssemblyPipeline{}).VerifyAndPublishSignedAssembly(
		ctx, delivery.ExpansionLease{}, delivery.PreparePlan{},
		delivery.DelegatedSigningClosureCoordinator{})
	if state != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled verification must not publish: state=%q err=%v", state, err)
	}
}
