package semanticproposal

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type nativePreparationBudget struct {
	OrdinaryPolicy                       *createProjectPolicy
	Costs                                v2input.SemanticQualifiedCosts
	RAM, VRAM, Scratch, Output, Reserved int64
}

// Composes owned request, current server policy, source-verified Params upload,
// trusted qualification and frozen task/anchor. It never admits or queues tasks.
func (s SQLTransferSourceReader) prepareDetectorDraftPlan(ctx context.Context, catalog v2draft.ArtifactCatalog, publisher v2draft.Publisher, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, color TransferColorProfile, report v2input.Ref, score float64, maxFaces int, budget nativePreparationBudget) (detectorDraftPlan, error) {
	var zero detectorDraftPlan
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return zero, err
	}
	if catalog == nil {
		return zero, v2input.ErrInvalid
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, owned.TenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	expected := selected["face.detect"]
	if expected.Backend != "cuda" {
		return zero, v2qualification.ErrUnqualified
	}
	document := uuid.NewSHA1(owned.RunID, []byte("semantic.face.detect.params:reference"))
	params, err := s.publishSourceDetectorVersionParams(ctx, who, owned.TenantID, owned.Intent.Reference, color, publisher, document, expected.ModelLockSHA, score, maxFaces)
	if err != nil {
		return zero, fmt.Errorf("publish detector Params: %w", err)
	}
	prepared, err := s.prepareSourceDetectorTask(ctx, catalog, who, owned.TenantID, owned.RunID, 0, owned.Intent.Reference, color, document, params, report, expected, score, maxFaces, budget.Costs, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
	if err != nil {
		return zero, fmt.Errorf("qualify detector task: %w", err)
	}
	if err := s.recheckNativePlanPolicy(ctx, who, owned.TenantID, policy); err != nil {
		return zero, err
	}
	return s.compileDetectorDraftPlan(ctx, who, owned, key, published, snapshot, snapshotBody, prepared, policy, budget.Reserved)
}
func (s SQLTransferSourceReader) preparePersonDraftPlan(ctx context.Context, catalog v2draft.ArtifactCatalog, publisher v2draft.Publisher, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, color TransferColorProfile, report v2input.Ref, tile int, budget nativePreparationBudget) (personDraftPlan, error) {
	var zero personDraftPlan
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return zero, err
	}
	if catalog == nil {
		return zero, v2input.ErrInvalid
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, owned.TenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	expected := selected["mask.person"]
	if expected.Backend != "cuda" {
		return zero, v2qualification.ErrUnqualified
	}
	document := uuid.NewSHA1(owned.RunID, []byte("semantic.mask.person.params:reference"))
	params, err := s.publishSourcePersonVersionParams(ctx, who, owned.TenantID, owned.Intent.Reference, color, publisher, document, expected.ModelLockSHA, tile)
	if err != nil {
		return zero, fmt.Errorf("publish person Params: %w", err)
	}
	prepared, err := s.prepareSourcePersonTask(ctx, catalog, who, owned.TenantID, owned.RunID, owned.Intent.Reference, color, document, params, report, expected, tile, budget.Costs, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
	if err != nil {
		return zero, fmt.Errorf("qualify person task: %w", err)
	}
	if err := s.recheckNativePlanPolicy(ctx, who, owned.TenantID, policy); err != nil {
		return zero, err
	}
	if budget.OrdinaryPolicy != nil {
		prepared.continuation = "ordinary_parent"
		prepared.ordinaryPolicy = budget.OrdinaryPolicy
	}
	return s.compilePersonDraftPlan(ctx, who, owned, key, published, snapshot, snapshotBody, prepared, policy, budget.Reserved)
}

func ownNativePlanRequest(ctx context.Context, who auth.Identity, request Request, key string) (Request, error) {
	if err := ctx.Err(); err != nil {
		return Request{}, err
	}
	owned, err := CompileRequest(request.TenantID, request.ActorID, key, request.Body)
	if err != nil {
		return Request{}, err
	}
	if who.UserID != owned.ActorID || who.SessionID == uuid.Nil || request.RunID != owned.RunID || request.WorkflowID != owned.WorkflowID || request.RequestSHA != owned.RequestSHA {
		return Request{}, v2input.ErrInvalid
	}
	return owned, nil
}
func (s SQLTransferSourceReader) recheckNativePlanPolicy(ctx context.Context, who auth.Identity, tenant uuid.UUID, retained qualificationPolicySnapshot) error {
	if !validQualificationPolicySnapshot(s, who, tenant, retained) {
		return v2input.ErrInvalid
	}
	current, err := s.readQualificationPolicySnapshot(ctx, who, tenant)
	if err != nil {
		return err
	}
	if current.body != retained.body {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
