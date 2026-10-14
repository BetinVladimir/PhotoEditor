package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Exact immutable pins for current result only; no content or admission claim.
func (s SQLTransferSourceReader) nativeSkinResultObjects(ctx context.Context, who auth.Identity, plan nativeSkinDraftPlan, catalog nativeSkinResultCatalog) ([4]defectEvidenceObject, error) {
	var zero [4]defectEvidenceObject
	if err := verifyNativeSkinPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil || len(catalog.Outputs) != 3 {
		return zero, v2input.ErrInvalid
	}
	owned, err := s.compileNativeSkinDraftPlan(ctx, who, plan.parser, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != plan.seal || catalog.Params != plan.prepared.params.ref {
		return zero, v2input.ErrConflict
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	r := plan.parser.detector.request
	refs := append(append([]v2input.Ref(nil), catalog.Outputs...), catalog.Params)
	media := []string{"application/octet-stream", "image/png", "application/json", "application/json"}
	var result [4]defectEvidenceObject
	err = tenant.WithTenant(ctx, s.DB, who, r.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.semantic_native_skin_result_objects($1,$2,$3,$4,$5,$6,$7,$8) ORDER BY ordinal", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.RunID, r.RequestSHA, task.ID, catalog.ResultID)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			if count >= 4 {
				return v2input.ErrConflict
			}
			entry := defectEvidenceObject{}
			var ordinal int
			var bucket string
			if err := rows.Scan(&ordinal, &entry.ref.ArtifactID, &entry.ref.SHA256, &bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes, &entry.object.ContentType); err != nil {
				return err
			}
			if ordinal != count || entry.ref != refs[count] || bucket != s.Bucket || entry.object.Bytes < 1 || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" || entry.object.ContentType != media[count] {
				return v2input.ErrConflict
			}
			entry.object.SHA256 = entry.ref.SHA256
			result[count] = entry
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 4 {
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
