package interchange

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
)

type ImportPersonMaskCandidateEntry struct {
	EntryID         uuid.UUID `json:"entry_id"`
	TargetVersionID uuid.UUID `json:"target_version_id"`
	AssetID         uuid.UUID `json:"asset_id"`
	ExternalID      string    `json:"external_id"`
}

func (s ImportPersonMaskCandidateService) ListEntries(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID uuid.UUID) ([]ImportPersonMaskCandidateEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Objects == nil || tenantID == uuid.Nil || projectID == uuid.Nil || workflowID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	var actualProject uuid.UUID
	err := database.TenantTransaction(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT project_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`, tenantID, workflowID).Scan(&actualProject)
	})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && actualProject != projectID {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return nil, err
	}
	parsedSource := SQLImportMaskCandidateParsedSource{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who, Bucket: s.Bucket}
	parsed, err := ReadImportReviewParsed(ctx, parsedSource, s.Objects)
	if err != nil {
		return nil, err
	}
	entries := []ImportPersonMaskCandidateEntry{}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT entry_id,asset_version_id,asset_id,external_id FROM photo.v30_import_mask_candidate_entries($1,$2,$3,$4,$5)`, tenantID, who.UserID, who.SessionID, workflowID, string(parsed))
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[uuid.UUID]bool{}
		for rows.Next() {
			var entry ImportPersonMaskCandidateEntry
			if err := rows.Scan(&entry.EntryID, &entry.TargetVersionID, &entry.AssetID, &entry.ExternalID); err != nil {
				return err
			}
			if entry.EntryID == uuid.Nil || entry.TargetVersionID == uuid.Nil || entry.AssetID == uuid.Nil || len(entry.ExternalID) < 1 || len(entry.ExternalID) > 240 || seen[entry.EntryID] || len(entries) >= 5000 {
				return artifactjson.ErrStale
			}
			seen[entry.EntryID] = true
			entries = append(entries, entry)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	current, err := ReadImportReviewParsed(ctx, parsedSource, s.Objects)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(current, parsed) {
		return nil, artifactjson.ErrStale
	}
	return entries, ctx.Err()
}
