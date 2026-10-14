package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Keeps selection distinct from protection and locks both original authorities.
// Completed evidence is byte-verified before the caller begins this transaction.
func (s SQLTransferSourceReader) lockNativeInputManualSelectionSeparation(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, source nativeInputManualSourceContinuation, proof nativeInputManualSelectionSeparationProof, color TransferColorProfile, completed json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	if !sameRegisteredNativeInputManualProtection(ctx, source.protection, proof.registered) {
		return v2input.ErrConflict
	}
	if _, err := composeNativeInputManualSourceContinuation(ctx, who, request, source.source, proof.registered, color); err != nil {
		return err
	}
	pins, err := s.marshalNativeInputManualSelectionPins(who, request, proof)
	if err != nil {
		return err
	}
	if err := s.lockNativeInputManualSourceContinuation(ctx, tx, who, request, source, color, completed); err != nil {
		return err
	}
	b := request.Intent.Reference
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_selection_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, b.CanvasHash, proof.root.ArtifactID, proof.root.SHA256, request.Intent.TargetSnapshotID, string(pins)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) marshalNativeInputManualSelectionPins(who auth.Identity, request Request, proof nativeInputManualSelectionSeparationProof) ([]byte, error) {
	parent := proof.registered.published.preparation.plan.prepared.stage.dependencies.dependencies.producers.detector
	r := parent.request
	sha, err := canonical.Hash(request.Intent)
	originalSHA, originalErr := canonical.Hash(r.Intent)
	if err != nil || originalErr != nil || sha != originalSHA || s.Bucket == "" || s.ProjectID == uuid.Nil || who.UserID != request.ActorID || who.SessionID == uuid.Nil || request.TenantID == uuid.Nil || proof.count <= 0 || proof.root != request.Intent.ReferenceMask || proof.root.ArtifactID == uuid.Nil || proof.pixels.ArtifactID == uuid.Nil || proof.root.ArtifactID == proof.pixels.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	if r.TenantID != request.TenantID || r.ActorID != who.UserID || r.RunID != request.RunID || r.RequestSHA != request.RequestSHA || parent.prepared.source.project != s.ProjectID || parent.prepared.source.bucket != s.Bucket || parent.prepared.source.session != who.SessionID {
		return nil, v2input.ErrConflict
	}
	return s.marshalSelectionObjectPins(proof.root, proof.pixels, proof.rootObject, proof.pixelsObject)
}
