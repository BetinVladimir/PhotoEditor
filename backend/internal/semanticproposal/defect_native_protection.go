package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Private publication input. Strings retain owned immutable document bytes;
// this is not a registered root and must not be passed to transfer consumers.
type nativeDefectProtectionPreparation struct {
	document uuid.UUID
	body     string
	report   string
	pixels   v2input.Ref
	parent   nativeDefectParentProof
}

func (s SQLTransferSourceReader) prepareNativeDefectProtection(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID) (nativeDefectProtectionPreparation, error) {
	var zero nativeDefectProtectionPreparation
	result, err := s.ReadNativeDefectResultForRequest(ctx, who, request, key, intent, snapshot, task)
	if err != nil {
		return zero, err
	}
	image, err := s.Read(ctx, who, request.TenantID, request.Intent.Reference)
	if err != nil {
		return zero, err
	}
	body, pixels, err := v2input.BuildSemanticDefectProtectionDocument(ctx, result.Pins.Params, result.Evidence.Params, result.Pins.Report, result.Evidence.Report, result.Pins.Proposal, result.Evidence.Proposal, request.Intent.Reference, image)
	if err != nil {
		return zero, err
	}
	if pixels != result.Pins.Protection {
		return zero, v2input.ErrInvalid
	}
	// Repeat full live parent/result/content authority after source IO. No SQL
	// transaction spans object IO; final qualification/admission locks are separate.
	after, err := s.ReadNativeDefectResultForRequest(ctx, who, request, key, intent, snapshot, task)
	if err != nil {
		return zero, err
	}
	if after.parentProof != result.parentProof {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	document := uuid.NewSHA1(request.RunID, []byte("semantic.defect.protection:"+task.String()))
	return nativeDefectProtectionPreparation{document: document, body: string(body), report: string(result.Evidence.Report), pixels: pixels, parent: result.parentProof}, nil
}
