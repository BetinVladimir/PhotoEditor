package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Locks selected original source artifacts only. Caller must compose auth,
// complete inventory/barrier and admission in this TX, rolling back on error.
func (s SQLTransferSourceReader) lockSourcePreparationProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, p sourcePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || p.tenant != tenant || p.project != s.ProjectID || p.actor != who.UserID || p.session != who.SessionID || p.bucket != s.Bucket || p.binding != binding {
		return v2input.ErrInvalid
	}
	body, err := marshalSourceEvidencePins(p)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_source_evidence($1,$2,$3,$4,$5,$6,$7,$8)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, string(body)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return nil
}

func marshalSourceEvidencePins(p sourcePreparationProof) ([]byte, error) {
	pins, err := decodeSourcePreparationPins(p)
	if err != nil {
		return nil, err
	}
	entries := make([]map[string]any, 0, len(pins))
	for _, pin := range pins {
		var parentID, parentSHA any
		if pin.Node.ParentRef.ArtifactID != uuid.Nil {
			parentID = pin.Node.ParentRef.ArtifactID
			parentSHA = pin.Node.ParentRef.SHA256
		}
		entries = append(entries, map[string]any{"contract": pin.Node.Contract, "artifact_id": pin.Node.Ref.ArtifactID, "sha256": pin.Node.Ref.SHA256, "bucket": p.bucket, "key": pin.Object.Key, "version": pin.Object.VersionID, "bytes": pin.Object.Bytes, "media_type": pin.Object.ContentType, "parent_id": parentID, "parent_sha": parentSHA})
	}
	return canonical.Marshal(entries)
}
