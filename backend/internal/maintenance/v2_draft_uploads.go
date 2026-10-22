package maintenance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

type V2DraftUploadTicket struct {
	TenantID, DocumentID, ArtifactID, Token uuid.UUID
	Bucket                                  string
}

type V2DraftUploadRepository interface {
	Claim(context.Context) (*V2DraftUploadTicket, error)
	Finish(context.Context, V2DraftUploadTicket) (bool, error)
	Retire(context.Context) (bool, error)
}

type V2DraftUploadObjects interface {
	PurgeV2Draft(context.Context, uuid.UUID, uuid.UUID) error
}

type V2DraftUploadCleaner struct {
	Repository V2DraftUploadRepository
	Objects    V2DraftUploadObjects
	Bucket     string
}

func (c V2DraftUploadCleaner) CleanOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil { return false, err }
	if c.Repository == nil || c.Objects == nil || c.Bucket == "" {
		return false, errors.New("invalid V2 draft upload cleanup configuration")
	}
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := c.Repository.Claim(claimCtx)
	claimErr := claimCtx.Err()
	cancel()
	if err == nil { err = claimErr }
	if err != nil {
		return false, err
	}
	if ticket == nil {
		retireCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		if err := retireCtx.Err(); err != nil { return false, err }
		retired, err := c.Repository.Retire(retireCtx)
		if err == nil { err = retireCtx.Err() }
		return retired, err
	}
	if ticket.DocumentID == uuid.Nil || ticket.Token == uuid.Nil ||
		ticket.Bucket != c.Bucket {
		return false, errors.New("invalid V2 draft upload cleanup ticket")
	}
	if _, err = objectstore.V2DraftKey(ticket.TenantID, ticket.ArtifactID); err != nil {
		return false, err
	}
	purgeCtx, stop := context.WithTimeout(ctx, 40*time.Second)
	err = c.Objects.PurgeV2Draft(purgeCtx, ticket.TenantID, ticket.ArtifactID)
	purgeErr := purgeCtx.Err()
	stop()
	if err != nil {
		return false, err
	}
	if purgeErr != nil { return false, purgeErr }
	finishCtx, finishDone := context.WithTimeout(ctx, 5*time.Second)
	defer finishDone()
	if err := finishCtx.Err(); err != nil { return false, err }
	accepted, err := c.Repository.Finish(finishCtx, *ticket)
	if err == nil { err = finishCtx.Err() }
	return accepted, err
}

type SQLV2DraftUploadCleanup struct{ DB *pgxpool.Pool }

func (s SQLV2DraftUploadCleanup) Claim(ctx context.Context) (*V2DraftUploadTicket, error) {
	var ticket V2DraftUploadTicket
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,document_id,artifact_id,bucket,cleanup_token
	 FROM photo.claim_v2_draft_upload_cleanup()`).Scan(&ticket.TenantID,
		&ticket.DocumentID, &ticket.ArtifactID, &ticket.Bucket, &ticket.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

func (s SQLV2DraftUploadCleanup) Finish(ctx context.Context, ticket V2DraftUploadTicket) (bool, error) {
	var done bool
	err := s.DB.QueryRow(ctx, `SELECT photo.finish_v2_draft_upload_cleanup($1,$2,$3)`,
		ticket.TenantID, ticket.DocumentID, ticket.Token).Scan(&done)
	return done, err
}

func (s SQLV2DraftUploadCleanup) Retire(ctx context.Context) (bool, error) {
	var done bool
	err := s.DB.QueryRow(ctx, `SELECT photo.retire_one_v2_draft_upload()`).Scan(&done)
	return done, err
}
