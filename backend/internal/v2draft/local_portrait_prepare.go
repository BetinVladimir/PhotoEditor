package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type PreparedLocalPortraitTask struct {
	Published v2portrait.PublishedLocalTaskPlan
	Evidence  TaskInputEvidence
}

// PrepareLocalPortraitTask verifies manual dependencies before publishing local
// Params, then repeats scoped closure with the published document. The caller
// must provide durable graph owner authority and separately verify mask pixels,
// runtime qualification and final transactional graph admission.
func (s ClosureResolver) PrepareLocalPortraitTask(ctx context.Context, who auth.Identity,
	tenant uuid.UUID, publisher v2input.DocumentPublisher, scope artifactjson.Scope,
	operation string, binding v2input.SessionVersionBinding, sourceRaw, localRaw []byte,
	runtimeSHA string, capacity v2portrait.LocalResourceEstimate, rootRef v2input.Ref, rootRaw []byte,
	originalParams v2input.Ref, originalRaw, vectorsRaw []byte, targetRef, guardRef v2input.Ref) (PreparedLocalPortraitTask, error) {
	var zero PreparedLocalPortraitTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if publisher == nil || scope.Tenant != tenant || scope.Tenant == uuid.Nil || scope.Run == uuid.Nil || scope.Owner == uuid.Nil || scope.Purpose != "task_params" {
		return zero, v2input.ErrInvalid
	}
	plan, err := v2portrait.CompileLocalTaskPlan(ctx, operation, binding, sourceRaw, localRaw, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	var header struct {
		Mask      v2input.Ref `json:"mask"`
		Protected v2input.Ref `json:"protected"`
	}
	if json.Unmarshal(plan.Params, &header) != nil || header.Mask != targetRef || header.Protected != guardRef {
		return zero, v2input.ErrInvalid
	}
	if _, err = s.ResolveManualRasterTaskInputEvidence(ctx, who, tenant, binding, sourceRaw, rootRef, rootRaw, originalParams, originalRaw, vectorsRaw, targetRef, guardRef); err != nil {
		return zero, err
	}
	published, err := v2portrait.CompileAndPublishLocalTaskPlan(ctx, publisher, scope, operation, binding, sourceRaw, plan.Params, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	evidence, err := s.ResolveLocalPortraitTaskInputEvidence(ctx, who, tenant, binding, published.ParamsRef, published.Plan.Params, sourceRaw, rootRef, rootRaw, originalParams, originalRaw, vectorsRaw, targetRef, guardRef)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return PreparedLocalPortraitTask{Published: published, Evidence: evidence}, nil
}
