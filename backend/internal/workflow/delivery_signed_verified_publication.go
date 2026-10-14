package workflow

import (
	"context"

	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// VerifyAndPublishSignedAssembly binds the documents to a freshly verified
// complete signer set. It never accepts caller-supplied slots or receipts.
// Registration and activation remain separate fenced operations.
func (p DeliveryAssemblyPipeline) VerifyAndPublishSignedAssembly(
	ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan,
	verify delivery.DelegatedSigningClosureCoordinator,
) (string, delivery.SignedAssemblyPublication, delivery.VerifiedDelegatedSigningSet, error) {
	var empty delivery.SignedAssemblyPublication
	var evidence delivery.VerifiedDelegatedSigningSet
	if err := ctx.Err(); err != nil {
		return "", empty, evidence, err
	}
	if p.DB == nil || p.Objects == nil || p.SignedObjects == nil ||
		len(p.Bucket) < 3 || len(p.Bucket) > 63 ||
		!lease.Valid() || lease.Cursor != lease.SlotCount ||
		lease.ParentTask != plan.TaskID() || verify.Store.DB == nil {
		return "", empty, evidence, v2input.ErrInvalid
	}
	state, checked, err := verify.VerifyAndRetainEvidence(ctx, plan, lease)
	if err != nil || state != "adopted" {
		return state, empty, evidence, err
	}
	choice, err := delivery.DeliverySigningChoice(checked.Frozen)
	if err != nil {
		return "", empty, evidence, err
	}
	if _, err := verify.Store.ReadRetainedSigningClosure(ctx, lease,
		checked.Actor, choice.PolicyID, choice.PolicySHA,
		checked.Slots, checked.Dependencies, checked.Receipts); err != nil {
		return "", empty, evidence, err
	}
	publication, err := PublishSignedDeliveryAssemblyBundle(ctx,
		deliveryAssemblyPublicationQueries{db: p.DB, lease: lease},
		p.Objects, p.Bucket, lease, plan, checked.Frozen,
		checked.SnapshotRaw, checked.Slots, checked.Actor,
		checked.Dependencies, checked.Receipts)
	if err != nil {
		return "", empty, evidence, err
	}
	if err := verify.Observations.Renew(ctx, lease); err != nil {
		return "", empty, evidence, err
	}
	if err := (delivery.ExpansionLeaseStore{DB: deliveryAssemblyPublicationQueries{
		db: p.DB, lease: lease}}).RetainPublishedAssembly(ctx, lease, plan,
		publication.AssemblyRef); err != nil {
		return "", empty, evidence, err
	}
	return "published", publication, checked, nil
}

// ProcessVerifiedSignedAssembly carries one verified matrix through signed
// publication, server-resolved transitive inputs, blocked registration and
// the final physical activation barrier.
func (p DeliveryAssemblyPipeline) ProcessVerifiedSignedAssembly(
	ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan,
	verify delivery.DelegatedSigningClosureCoordinator,
) (string, error) {
	state, publication, evidence, err := p.VerifyAndPublishSignedAssembly(ctx, lease, plan, verify)
	if err != nil || state != "published" { return state, err }
	resolved, err := (delivery.ExpansionLeaseStore{DB: deliveryAssemblyRecoveryQueries{
		deliveryAssemblyObservationQueries: deliveryAssemblyObservationQueries{db: p.DB, lease: lease},
		root: publication.AssemblyRef,
	}}).ResolvePublishedSignedAssemblyInputs(ctx, lease, plan, publication, evidence)
	if err != nil { return "", err }
	if err := p.RegisterAndActivateSignedAssembly(ctx, lease, plan, publication,
		evidence.Actor, evidence.Slots, evidence.Dependencies,
		evidence.Receipts, resolved); err != nil { return "", err }
	return "activated", nil
}
