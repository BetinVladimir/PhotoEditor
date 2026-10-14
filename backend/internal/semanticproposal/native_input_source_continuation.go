package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInputSourceContinuation struct {
	source     sourcePreparationProof
	protection registeredNativeInputProtection
}

// This binds source and defect protection only. Person coverage, transfer model,
// target authority and parent graph admission are separate mandatory contracts.
func composeNativeInputSourceContinuation(ctx context.Context, who auth.Identity, request Request, source sourcePreparationProof, protection registeredNativeInputProtection, color TransferColorProfile) (nativeInputSourceContinuation, error) {
	var zero nativeInputSourceContinuation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	p := protection.published.preparation
	if err := verifyNativeDefectDraftSeal(ctx, p.plan); err != nil {
		return zero, err
	}
	parent := p.plan.prepared.dependencies.preparation.landmarksPlan.detector
	r := parent.request
	if source.tenant == uuid.Nil || source.project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || source.tenant != request.TenantID || source.actor != who.UserID || source.session != who.SessionID || source.bucket == "" || source.iccSHA != color.sha || source.unitRGB || source.binding != request.Intent.Reference {
		return zero, v2input.ErrInvalid
	}
	if r.RunID != request.RunID || r.RequestSHA != request.RequestSHA || r.ActorID != who.UserID || source.project != parent.prepared.source.project || !nativeTransferSourceMatchesDetector(source, parent.prepared.source) {
		return zero, v2input.ErrConflict
	}
	if protection.verified.Root != protection.published.root || protection.verified.Pixels != p.pixels || string(protection.verified.Body) != p.body {
		return zero, v2input.ErrConflict
	}
	if _, err := marshalSourceEvidencePins(source); err != nil {
		return zero, err
	}
	return nativeInputSourceContinuation{source: source, protection: protection}, nil
}

// Locks complete original source closure and new-native protected result in the
// same caller transaction. Completed bytes must be verified before SQL starts.
func (s SQLTransferSourceReader) lockNativeInputSourceContinuation(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, proof nativeInputSourceContinuation, color TransferColorProfile, completed json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	if _, err := composeNativeInputSourceContinuation(ctx, who, request, proof.source, proof.protection, color); err != nil {
		return err
	}
	if err := s.lockSourcePreparationProof(ctx, tx, who, request.TenantID, request.Intent.Reference, proof.source); err != nil {
		return err
	}
	return s.lockRegisteredNativeInputProtection(ctx, tx, who, proof.protection, completed)
}

// Detector inputs are validated in [0,1]; transfer inputs use the working-image
// range. Both proofs must retain exactly the same scope and immutable objects.
// The caller independently verifies transfer pixels; no detector plan is changed.
func nativeTransferSourceMatchesDetector(source, detector sourcePreparationProof) bool {
	if source.unitRGB {
		return false
	}
	detector.unitRGB = false
	return source == detector
}
