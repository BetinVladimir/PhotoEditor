package interchange

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
)

type ImportPersonMaskCandidateService struct {
	DB      *pgxpool.Pool
	Objects ImportDocumentObjects
	Bucket  string
}

func (s ImportPersonMaskCandidateService) ReadEntry(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID, entryID uuid.UUID) ([]ImportPersonMaskCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Objects == nil || tenantID == uuid.Nil || projectID == uuid.Nil || workflowID == uuid.Nil || entryID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
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
	targetSource := SQLImportMaskCandidateTarget{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who}
	target, err := targetSource.Resolve(ctx, parsed, entryID)
	if err != nil {
		return nil, err
	}
	archive := SQLImportMaskCandidateArchiveReader{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who, Bucket: s.Bucket, Parsed: parsed, Objects: s.Objects}
	candidates, err := ExtractImportPersonMaskCandidates(ctx, parsed, entryID, target, archive)
	if err != nil {
		return nil, err
	}
	currentParsed, err := ReadImportReviewParsed(ctx, parsedSource, s.Objects)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(currentParsed, parsed) {
		return nil, artifactjson.ErrStale
	}
	currentTarget, err := targetSource.Resolve(ctx, currentParsed, entryID)
	if err != nil {
		return nil, err
	}
	if currentTarget != target {
		return nil, artifactjson.ErrStale
	}
	return candidates, ctx.Err()
}
