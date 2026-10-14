package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Reference-side source authority; targets and complete graph admission are
// independent. This carries the original verified RGB/alpha/warp closure.
type nativeSourceContinuationProof struct {
	source sourcePreparationProof
	native nativeReferenceContinuationProof
}

func matchNativeSourceScope(source sourcePreparationProof, native nativeReferenceContinuationProof, color TransferColorProfile) error {
	origin := native.defect.parent.result.origin
	if source.tenant == uuid.Nil || source.project == uuid.Nil || source.actor == uuid.Nil || source.session == uuid.Nil || source.bucket == "" || source.tenant != origin.tenant || source.project != origin.project || source.actor != origin.actor || source.session != origin.session || source.bucket != origin.bucket || source.binding != origin.binding || source.iccSHA != color.sha || source.unitRGB {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(color.sha); err != nil {
		return v2input.ErrInvalid
	}
	_, err := marshalSourceEvidencePins(source)
	return err
}

func composeNativeSourceContinuation(source sourcePreparationProof, native nativeReferenceContinuationProof, color TransferColorProfile) (nativeSourceContinuationProof, error) {
	if err := matchNativeSourceScope(source, native, color); err != nil {
		return nativeSourceContinuationProof{}, err
	}
	owned, err := composeNativeReferenceContinuation(native.defect, native.protection)
	if err != nil {
		return nativeSourceContinuationProof{}, err
	}
	return nativeSourceContinuationProof{source: source, native: owned}, nil
}

// Locks the exact original source closure and native evidence together. No
// object IO/commit; caller must roll back every error and qualify all targets.
func (s SQLTransferSourceReader) lockNativeSourceContinuation(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, proof nativeSourceContinuationProof, color TransferColorProfile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || proof.source.binding != request.Intent.Reference {
		return v2input.ErrInvalid
	}
	if err := matchNativeSourceScope(proof.source, proof.native, color); err != nil {
		return err
	}
	if err := matchNativeReferenceContinuation(proof.native); err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", request.TenantID.String()); err != nil {
		return err
	}
	if err := s.lockSourcePreparationProof(ctx, tx, who, request.TenantID, request.Intent.Reference, proof.source); err != nil {
		return err
	}
	return s.lockNativeReferenceContinuation(ctx, tx, who, request, proof.native)
}
