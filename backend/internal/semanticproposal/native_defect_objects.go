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
func (s SQLTransferSourceReader) nativeInputDefectResultObjects(ctx context.Context, who auth.Identity, plan nativeDefectDraftPlan, catalog nativeInputDefectResultCatalog) ([6]defectEvidenceObject, error) {
	var zero [6]defectEvidenceObject
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if s.DB == nil || len(catalog.Outputs) != 5 {
		return zero, v2input.ErrInvalid
	}
	owned, err := s.compileNativeDefectDraftPlan(ctx, who, plan.prepared, plan.shell.Bytes)
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
	r := plan.prepared.dependencies.preparation.landmarksPlan.detector.request
	refs := append(append([]v2input.Ref(nil), catalog.Outputs...), catalog.Params)
	media := []string{"application/octet-stream", "application/octet-stream", "image/png", "application/json", "application/json", "application/json"}
	maximum := []int64{401048576, 801048576, 613048576, 1048576, 1048576, 1048576}
	var result [6]defectEvidenceObject
	err = tenant.WithTenant(ctx, s.DB, who, r.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.semantic_native_defect_result_objects($1,$2,$3,$4,$5,$6,$7,$8) ORDER BY ordinal", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.RunID, r.RequestSHA, task.ID, catalog.ResultID)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			if count >= 6 {
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
		if count != 6 {
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
