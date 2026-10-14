package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Binds exact retained text to independently locked original producer and newly
// published document metadata. Does not seal or activate a task.
func (s SQLTransferSourceReader) lockDefectContentProvenance(ctx context.Context, tx pgx.Tx, who auth.Identity, plan defectPreparationDraftPlan, content json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	expected, err := marshalDefectActivationContent(ctx, plan)
	if err != nil {
		return err
	}
	if string(content) != string(expected) {
		return v2input.ErrInvalid
	}
	binding := plan.request.Intent.Reference
	origin, err := s.marshalDefectOriginEvidencePins(who, plan.request.TenantID, binding, plan.execution.execution.face.originProof)
	if err != nil {
		return err
	}
	object := plan.params.object
	pin, err := canonical.Marshal(map[string]any{"key": object.Key, "version": object.VersionID, "sha256": object.SHA256, "bytes": object.Bytes, "media_type": object.ContentType, "bucket": s.Bucket})
	if err != nil {
		return err
	}
	var matches bool
	err = tx.QueryRow(ctx, `SELECT photo.lock_semantic_defect_content_provenance($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::jsonb,$13::jsonb)`, plan.request.TenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, plan.params.document, plan.params.params.ArtifactID, plan.params.params.SHA256, string(origin), string(pin), string(content)).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
