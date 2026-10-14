package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Caller owns the transaction and must roll back every error. Locks current
// source authorization, original SFD and exact canonical parser objects; no IO.
func (s SQLTransferSourceReader) lockNativeSkinDependencyInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeSkinDraftPlan) error {
	if tx == nil {
		return v2input.ErrInvalid
	}
	packet, err := s.marshalNativeSkinDependencyEvidence(ctx, who, plan)
	if err != nil {
		return err
	}
	owned := plan
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", owned.shell.Tenant.String()); err != nil {
		return err
	}
	r := owned.parser.detector.request
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_parse_result($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.RunID, r.RequestSHA, owned.parser.shell.Tasks[0].ID, string(packet)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Bounded immutable dependency packet for composition with final skin admission.
// Serialization does not grant SQL authority or activate a task.
func (s SQLTransferSourceReader) marshalNativeSkinDependencyEvidence(ctx context.Context, who auth.Identity, plan nativeSkinDraftPlan) (json.RawMessage, error) {
	if err := verifyNativeSkinPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	owned, err := s.compileNativeSkinDraftPlan(ctx, who, plan.parser, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if owned.seal != plan.seal {
		return nil, v2input.ErrConflict
	}
	c := owned.prepared.params.parser
	detector, err := s.marshalCompletedDetectorLockEvidence(ctx, who, owned.parser.detector, c.detector)
	if err != nil {
		return nil, err
	}
	for i, body := range []string{c.body, c.paramsBody} {
		entry := c.objects[i]
		digest := sha256.Sum256([]byte(body))
		if len(body) < 2 || len(body) > 1<<20 || entry.object.Bytes != int64(len(body)) || entry.object.ContentType != "application/json" || hex.EncodeToString(digest[:]) != entry.ref.SHA256 {
			return nil, v2input.ErrConflict
		}
	}
	files := make([]nativeDetectorResultLockPin, 3)
	for i, entry := range []defectEvidenceObject{owned.prepared.labels, c.objects[0], c.objects[1]} {
		role := []string{"face_labels", "face_labels_ref", "parser_params"}[i]
		files[i] = nativeDetectorResultLockPin{role, entry.ref.ArtifactID, entry.ref.SHA256, s.Bucket, entry.object.Key, entry.object.VersionID, entry.object.Bytes, entry.object.ContentType}
	}
	packet, err := canonical.Marshal(map[string]any{"schema_version": 1, "result_id": c.catalog.ResultID, "outputs": c.catalog.Outputs, "params": c.catalog.Params, "files": files, "detector": json.RawMessage(detector)})
	if err != nil || len(packet) > 32768 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packet, nil
}
