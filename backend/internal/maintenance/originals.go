package maintenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"time"
)

type OriginalCleanup struct {
	ArtifactID, Token, TenantID uuid.UUID
	Bucket                      string
}
type OriginalCleanupRepository interface {
	Claim(context.Context) (*OriginalCleanup, error)
	Finish(context.Context, OriginalCleanup) (bool, error)
}
type OriginalObjects interface {
	PurgeOriginal(context.Context, uuid.UUID, uuid.UUID) error
}
type OriginalCleaner struct {
	Repository OriginalCleanupRepository
	Objects    OriginalObjects
	Bucket     string
}

func (c OriginalCleaner) CleanOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil { return false, err }
	if c.Repository == nil || c.Objects == nil || c.Bucket == "" {
		return false, errors.New("invalid original cleanup configuration")
	}
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := c.Repository.Claim(claim)
	claimErr := claim.Err()
	cancel()
	if err == nil { err = claimErr }
	if err != nil || ticket == nil {
		return false, err
	}
	if _, err = objectstore.OriginalKey(ticket.TenantID, ticket.ArtifactID); err != nil || ticket.Token == uuid.Nil || ticket.Bucket != c.Bucket {
		return false, errors.New("invalid original cleanup ticket")
	}
	purge, cancel := context.WithTimeout(ctx, 40*time.Second)
	err = c.Objects.PurgeOriginal(purge, ticket.TenantID, ticket.ArtifactID)
	purgeErr := purge.Err()
	cancel()
	if err != nil {
		return false, err
	}
	if purgeErr != nil { return false, purgeErr }
	finish, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := finish.Err(); err != nil { return false, err }
	accepted, err := c.Repository.Finish(finish, *ticket)
	if err == nil { err = finish.Err() }
	return accepted, err
}

type SQLOriginalCleanup struct{ DB artifactjson.RowQuerier }

func (s SQLOriginalCleanup) Claim(ctx context.Context) (*OriginalCleanup, error) {
	if s.DB == nil {
		return nil, errors.New("original cleanup database required")
	}
	ticket := new(OriginalCleanup)
	err := s.DB.QueryRow(ctx, "SELECT artifact_id,cleanup_token,tenant_id,bucket FROM photo.claim_original_cleanup()").Scan(&ticket.ArtifactID, &ticket.Token, &ticket.TenantID, &ticket.Bucket)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ticket, nil
}
func (s SQLOriginalCleanup) Finish(ctx context.Context, ticket OriginalCleanup) (bool, error) {
	if s.DB == nil || ticket.ArtifactID == uuid.Nil || ticket.Token == uuid.Nil {
		return false, errors.New("invalid original cleanup completion")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, "SELECT photo.finish_original_cleanup($1,$2)", ticket.ArtifactID, ticket.Token).Scan(&ok)
	return ok, err
}
