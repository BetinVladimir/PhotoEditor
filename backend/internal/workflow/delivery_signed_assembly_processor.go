package workflow

import (
	"context"

	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// SignedDeliveryAssemblyProcessor is the required-policy branch selected only
// after the signer stage has returned a complete adopted matrix. It repeats
// verification before publication and activation under the current lease.
type SignedDeliveryAssemblyProcessor struct {
	Pipeline DeliveryAssemblyPipeline
	Verifier delivery.DelegatedSigningClosureCoordinator
}

func (p SignedDeliveryAssemblyProcessor) ProcessAssembly(ctx context.Context,
	lease delivery.ExpansionLease, plan delivery.PreparePlan) (string, error) {
	mode, err := plan.ProvenanceMode()
	if err != nil { return "", err }
	if mode != "required" { return "", v2input.ErrInvalid }
	return p.Pipeline.ProcessVerifiedSignedAssembly(ctx, lease, plan, p.Verifier)
}

var _ DeliveryAssemblyProcessor = SignedDeliveryAssemblyProcessor{}
