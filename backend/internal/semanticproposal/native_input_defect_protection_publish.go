package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type publishedNativeInputDefectProtection struct {
	root        v2input.Ref
	preparation nativeInputDefectProtectionPreparation
}

// Publication is accepted-version scoped and quota fenced, not registered
// producer authority. Final registration/continuation must lock the original
// completed result and exact published object in a separate SQL transaction.
func (s SQLTransferSourceReader) publishNativeInputDefectProtection(ctx context.Context, who auth.Identity, plan nativeDefectDraftPlan, color TransferColorProfile, publisher v2draft.Publisher) (publishedNativeInputDefectProtection, error) {
	var zero publishedNativeInputDefectProtection
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if publisher.DB == nil || publisher.Objects == nil || publisher.Bucket != s.Bucket || s.Bucket == "" {
		return zero, v2input.ErrInvalid
	}
	prepared, err := s.prepareNativeInputDefectProtection(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	parent := plan.prepared.dependencies.preparation.landmarksPlan.detector.request
	image := []byte(plan.prepared.dependencies.preparation.landmarks.detector.sourceBody)
	root, err := publisher.PublishSemanticProtectionDocument(ctx, who, parent.TenantID, prepared.document, parent.Intent.Reference, image, []byte(prepared.body))
	if err != nil {
		return zero, err
	}
	sum := sha256.Sum256([]byte(prepared.body))
	if root.ArtifactID == uuid.Nil || root.ArtifactID == prepared.pixels.ArtifactID || root.SHA256 != hex.EncodeToString(sum[:]) {
		return zero, v2input.ErrConflict
	}
	after, err := s.prepareNativeInputDefectProtection(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if after.document != prepared.document || after.body != prepared.body || after.pixels != prepared.pixels || !reflect.DeepEqual(after.result, prepared.result) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return publishedNativeInputDefectProtection{root: root, preparation: prepared}, nil
}
