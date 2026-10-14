package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Locks privately retained original producer evidence only. Caller composes
// source, authorization, model policy and admission; failures require rollback.
func (s SQLTransferSourceReader) lockDefectOriginPreparationProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proof defectOriginPreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !s.validDefectOriginPreparationProof(who, tenant, binding, proof) {
		return v2input.ErrInvalid
	}
	body, err := s.marshalDefectOriginEvidencePins(who, tenant, binding, proof)
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
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_origin_evidence($1,$2,$3,$4,$5,$6,$7,$8)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, string(body)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Reusable exact origin pins for SQL content/provenance composition. Validation
// happens before serialization; this envelope alone grants no authority.
func (s SQLTransferSourceReader) marshalDefectOriginEvidencePins(who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proof defectOriginPreparationProof) ([]byte, error) {
	if !s.validDefectOriginPreparationProof(who, tenant, binding, proof) {
		return nil, v2input.ErrInvalid
	}
	roles := [5]string{"params", "proposal", "report", "labels", "protection"}
	entries := make([]map[string]any, 0, 5)
	for i, pin := range proof.objects {
		entries = append(entries, map[string]any{"role": roles[i], "artifact_id": pin.ref.ArtifactID, "sha256": pin.ref.SHA256, "bucket": proof.bucket, "key": pin.object.Key, "version": pin.object.VersionID, "bytes": pin.object.Bytes, "media_type": pin.object.ContentType})
	}
	body, err := canonical.Marshal(entries)
	if err != nil || len(body) > 4<<20 {
		return nil, v2input.ErrInvalid
	}
	return body, nil
}
