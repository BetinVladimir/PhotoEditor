package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Completed packet and registered proof must be physically verified before SQL.
// Caller rolls back every error. This is reference authority, not parent admission.
func (s SQLTransferSourceReader) lockRegisteredNativeInputManualInitialProtection(ctx context.Context, tx pgx.Tx, who auth.Identity, proof registeredNativeInputManualInitialProtection, completed json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	p := proof.published.preparation
	if proof.verified.Root != proof.published.root || proof.verified.Pixels != p.pixels || string(proof.verified.Body) != p.body {
		return v2input.ErrConflict
	}
	if err := s.lockNativeCompletedManualInitialDefectInTransaction(ctx, tx, who, p.plan, completed); err != nil {
		return err
	}
	parent := p.plan.prepared.stage.dependencies.dependencies.producers.detector
	r := parent.request
	b := r.Intent.Reference
	anchor, err := canonical.Marshal(map[string]any{"parent_run_id": r.RunID, "request_sha": r.RequestSHA, "auth_session_id": who.SessionID, "source": b, "intent": parent.published.Ref, "snapshot": parent.anchor.snapshotRef})
	if err != nil {
		return err
	}
	pins := make([]map[string]any, 2)
	for i, e := range proof.objects {
		pins[i] = map[string]any{"ordinal": i, "artifact_id": e.ref.ArtifactID, "sha256": e.ref.SHA256, "bucket": s.Bucket, "key": e.object.Key, "version": e.object.VersionID, "bytes": e.object.Bytes, "media_type": e.object.ContentType}
	}
	expected, err := canonical.Marshal(pins)
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_manual_input_protection_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13::jsonb,$14::jsonb)", r.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, p.plan.shell.Tasks[0].ID, p.result.catalog.ResultID, string(anchor), proof.verified.Root.ArtifactID, proof.verified.Root.SHA256, string(expected), string(completed)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
