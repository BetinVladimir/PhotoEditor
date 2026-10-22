package maintenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"time"
)

type StagingCleanup struct {
	ID, Token, TenantID, ArtifactID uuid.UUID
	Bucket                          string
	Object                          objectstore.Object
}
type StagingCleanupRepository interface {
	Claim(context.Context) (*StagingCleanup, error)
	Finish(context.Context, StagingCleanup) (bool, error)
}
type StagingObjects interface {
	RemoveConfirmed(context.Context, objectstore.Object) error
}
type StagingCleaner struct {
	Repository StagingCleanupRepository
	Objects    StagingObjects
	Bucket     string
}

func (c StagingCleaner) CleanOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil { return false, err }
	if c.Repository == nil || c.Objects == nil || c.Bucket == "" {
		return false, errors.New("invalid staging cleanup configuration")
	}
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := c.Repository.Claim(claim)
	claimErr := claim.Err()
	cancel()
	if err == nil { err = claimErr }
	if err != nil || ticket == nil {
		return false, err
	}
	if ticket.ID == uuid.Nil || ticket.Token == uuid.Nil || ticket.TenantID == uuid.Nil || ticket.ArtifactID == uuid.Nil || ticket.Bucket != c.Bucket || ticket.Object.Key == "" || ticket.Object.VersionID == "" || ticket.Object.VersionID == "null" || ticket.Object.Bytes < 1 || ticket.Object.Bytes > 536870912 || ticket.Object.ContentType == "" {
		return false, errors.New("invalid staging cleanup ticket")
	}
	if _, err = domain.ParseDigest(ticket.Object.SHA256); err != nil {
		return false, errors.New("invalid staging cleanup digest")
	}
	purge, cancel := context.WithTimeout(ctx, 40*time.Second)
	err = c.Objects.RemoveConfirmed(purge, ticket.Object)
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

type SQLStagingCleanup struct{ DB artifactjson.RowQuerier }

func (s SQLStagingCleanup) Claim(ctx context.Context) (*StagingCleanup, error) {
	if s.DB == nil {
		return nil, errors.New("staging cleanup database required")
	}
	t := new(StagingCleanup)
	err := s.DB.QueryRow(ctx, "SELECT cleanup_id,cleanup_token,tenant_id,artifact_id,bucket,object_key,object_version,sha256,bytes,media_type FROM photo.claim_staging_cleanup()").Scan(&t.ID, &t.Token, &t.TenantID, &t.ArtifactID, &t.Bucket, &t.Object.Key, &t.Object.VersionID, &t.Object.SHA256, &t.Object.Bytes, &t.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}
func (s SQLStagingCleanup) Finish(ctx context.Context, t StagingCleanup) (bool, error) {
	if s.DB == nil || t.ID == uuid.Nil || t.Token == uuid.Nil {
		return false, errors.New("invalid staging cleanup completion")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, "SELECT photo.finish_staging_cleanup($1,$2)", t.ID, t.Token).Scan(&ok)
	return ok, err
}
