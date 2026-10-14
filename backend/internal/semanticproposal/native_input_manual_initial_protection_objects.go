package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/canonical"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Exact immutable pins for current result only; no content or admission claim.
func (s SQLTransferSourceReader) registeredNativeInputManualInitialProtectionObjects(ctx context.Context, who auth.Identity, plan nativeInputManualInitialDraftPlan, published publishedNativeInputManualInitialProtection) ([2]defectEvidenceObject, error) {
	var zero [2]defectEvidenceObject
	catalog := published.preparation.result.catalog
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil || len(catalog.Outputs) != 5 {
		return zero, v2input.ErrInvalid
	}
	owned, err := s.compileNativeInputManualInitialDraft(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != plan.seal || catalog.Params != plan.prepared.stage.dependencies.params.params.ref {
		return zero, v2input.ErrConflict
	}
	task, err := plan.prepared.compiled.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	r := plan.prepared.stage.dependencies.dependencies.producers.detector.request
	refs := []v2input.Ref{published.root, catalog.Outputs[1]}
	media := []string{"application/json", "application/octet-stream"}
	maximum := []int64{1048576, 801048576}
	var result [2]defectEvidenceObject
	parent := plan.prepared.stage.dependencies.dependencies.producers.detector
	anchor, err := canonical.Marshal(map[string]any{"parent_run_id": r.RunID, "request_sha": r.RequestSHA, "auth_session_id": who.SessionID, "source": r.Intent.Reference, "intent": parent.published.Ref, "snapshot": parent.anchor.snapshotRef})
	if err != nil {
		return zero, err
	}
	err = tenant.WithTenant(ctx, s.DB, who, r.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.semantic_native_input_protection_objects($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12) ORDER BY ordinal", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.Intent.Reference.AssetVersionID, r.Intent.Reference.Source.ArtifactID, r.Intent.Reference.Source.SHA256, task.ID, catalog.ResultID, string(anchor), published.root.ArtifactID, published.root.SHA256)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			if count >= 2 {
				return v2input.ErrConflict
			}
			entry := defectEvidenceObject{}
			var ordinal int
			var bucket string
			if err := rows.Scan(&ordinal, &entry.ref.ArtifactID, &entry.ref.SHA256, &bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes, &entry.object.ContentType); err != nil {
				return err
			}
			if ordinal != count || entry.ref != refs[count] || bucket != s.Bucket || entry.object.Bytes < 1 || entry.object.Bytes > maximum[count] || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" || entry.object.ContentType != media[count] {
				return v2input.ErrConflict
			}
			entry.object.SHA256 = entry.ref.SHA256
			result[count] = entry
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 2 {
			return v2input.ErrConflict
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return result, nil
}
