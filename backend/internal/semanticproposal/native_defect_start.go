package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeDefectSubmission struct {
	plan   nativeDefectDraftPlan
	ticket v2input.JobTicket
}

// Initial submission from owned completed FAN/skin preparation. Existing
// registered submissions must use resume; errors expose neither plan nor ticket.
func (s SQLTransferSourceReader) startNativeDefectPreparation(ctx context.Context, scheduler *pgxpool.Pool, documents v2draft.Publisher, transport v2input.Publisher, who auth.Identity, prepared nativeDefectFacePreparation, color TransferColorProfile, report v2input.Ref, costs v2input.SemanticQualifiedCosts, landmarkBuilt, skinBuilt, parserBuilt, detectorBuilt v2input.Built, ram, vram, scratch, output, reserved int64) (nativeDefectSubmission, error) {
	var zero nativeDefectSubmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil || transport == nil || ram < 1 || vram < 0 || scratch < 1 || output < 1 || output > 9007199254740991-8388608 || reserved < output+8388608 || reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	additional, err := nativeDefectPersonInputPins(ctx, prepared)
	if err != nil {
		return zero, err
	}
	if err := validateNativeDefectBlockedInputAuthority(ctx, prepared, additional); err != nil {
		return zero, err
	}
	dependencies, err := s.prepareNativeDefectDependencies(ctx, who, prepared, color, landmarkBuilt, skinBuilt, parserBuilt, detectorBuilt)
	if err != nil {
		return zero, err
	}
	qualification, err := s.prepareNativeDefectStageQualification(ctx, who, prepared, color, report, costs)
	if err != nil {
		return zero, err
	}
	published, err := s.publishNativeDefectVersionParams(ctx, who, prepared, color, documents)
	if err != nil {
		return zero, err
	}
	parent := prepared.landmarksPlan.detector.request
	document := uuid.NewSHA1(parent.RunID, []byte("semantic.defect.params:reference"))
	object, err := s.defectVersionParamsObject(ctx, who, parent.TenantID, parent.Intent.Reference, document, published.Ref)
	if err != nil {
		return zero, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, published.Ref)
	if err != nil {
		return zero, err
	}
	if string(body) != prepared.params.params {
		return zero, v2input.ErrConflict
	}
	task, err := s.compileNativeDefectTask(ctx, who, dependencies, qualification, defectEvidenceObject{ref: published.Ref, object: object}, additional, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	plan, err := s.compileNativeDefectDraftPlan(ctx, who, task, reserved)
	if err != nil {
		return zero, err
	}
	if _, err := s.persistNativeDefectPreparationInputs(ctx, scheduler, transport, who, plan, color); err != nil {
		return zero, err
	}
	ticket, err := s.resumeNativeDefectPreparation(ctx, scheduler, who, plan)
	if err != nil {
		return zero, err
	}
	return nativeDefectSubmission{plan: plan, ticket: ticket}, nil
}
