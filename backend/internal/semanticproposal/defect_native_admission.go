package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Private evidence created only after complete native content/provenance reads.
// Qualification, parent identity and review admission remain separate obligations.
type nativeDefectAdmissionProof struct {
	task    uuid.UUID
	catalog nativeDefectResultCatalog
	origin  defectOriginPreparationProof
	png     defectEvidenceObject
}

// DB-only final canonical-result lock. Caller must rollback on error and compose
// qualification/input-provenance locks before any parent or review mutation.
func (s SQLTransferSourceReader) lockNativeDefectResult(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proof nativeDefectAdmissionProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, o := proof.catalog, proof.origin
	if tx == nil || proof.task == uuid.Nil || c.ResultID == uuid.Nil || !s.validDefectOriginPreparationProof(who, tenant, binding, o) || c.Params != o.pins.Params || c.Outputs != [5]v2input.Ref{o.pins.Labels, o.pins.Protection, proof.png.ref, o.pins.Proposal, o.pins.Report} || proof.png.ref.ArtifactID == uuid.Nil || proof.png.object.SHA256 != proof.png.ref.SHA256 || proof.png.object.ContentType != "image/png" || proof.png.object.Key == "" || proof.png.object.VersionID == "" || proof.png.object.VersionID == "null" || proof.png.object.Bytes < 57 || proof.png.object.Bytes > 613048576 {
		return v2input.ErrInvalid
	}
	origin, err := s.marshalDefectOriginEvidencePins(who, tenant, binding, o)
	if err != nil {
		return err
	}
	pin := proof.png.object
	expected, err := canonical.Marshal(map[string]any{"result_id": c.ResultID, "outputs": c.Outputs, "params": c.Params, "png": map[string]any{"artifact_id": proof.png.ref.ArtifactID, "sha256": proof.png.ref.SHA256, "bucket": s.Bucket, "key": pin.Key, "version": pin.VersionID, "bytes": pin.Bytes, "media_type": pin.ContentType}})
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
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_native_result($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, proof.task, string(origin), string(expected)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
