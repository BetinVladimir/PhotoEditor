package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// DB-only component; caller must compose full policy/source/graph admission and
// rollback the complete transaction on any refusal. No object/queue IO here.
func (s SQLTransferSourceReader) lockNativePersonProtection(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof nativePersonProtectionProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var expected v2qualification.Expected
	if tx == nil || proof.root.ArtifactID == uuid.Nil || proof.pixels.ArtifactID == uuid.Nil || proof.count < 0 || proof.evidence == "" || proof.qualification == "" || json.Unmarshal([]byte(proof.expected), &expected) != nil || expected.Operation != "mask.person" || expected.Version != "1" {
		return v2input.ErrInvalid
	}
	if err := s.lockNativeDefectContinuation(ctx, tx, who, tenantID, binding, proof.registered.parent); err != nil {
		return err
	}
	objects := proof.registered.objects
	if objects[1] != proof.registered.parent.result.origin.objects[4] || objects[0].ref.ArtifactID == uuid.Nil {
		return v2input.ErrInvalid
	}
	entries := make([]map[string]any, 2)
	for i, pin := range objects {
		entries[i] = map[string]any{"ordinal": i, "artifact_id": pin.ref.ArtifactID, "sha256": pin.ref.SHA256, "bucket": s.Bucket, "key": pin.object.Key, "version": pin.object.VersionID, "bytes": pin.object.Bytes, "media_type": pin.object.ContentType}
	}
	raw, err := canonical.Marshal(entries)
	if err != nil {
		return err
	}
	var ready bool
	native := proof.registered.parent.result
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_protection_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13::jsonb)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, native.task, native.catalog.ResultID, proof.registered.parent.anchor, objects[0].ref.ArtifactID, objects[0].ref.SHA256, string(raw)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, proof.root.ArtifactID, proof.root.SHA256, proof.pixels.ArtifactID, proof.pixels.SHA256, proof.expected, proof.evidence).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_person_qualification($1,$2,$3,$4,$5,$6)", tenantID, s.ProjectID, who.UserID, who.SessionID, proof.expected, proof.qualification).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
