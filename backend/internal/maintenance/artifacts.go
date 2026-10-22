package maintenance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
)

type ArtifactGC struct {
	ArtifactID, Token, TenantID uuid.UUID
	Bucket                      string
	Object                      objectstore.Object
}
type ArtifactGCRepository interface {
	Recover(context.Context) (int, error)
	Claim(context.Context) (*ArtifactGC, error)
	Finish(context.Context, ArtifactGC) (bool, error)
}
type ArtifactGCObjects interface {
	RemoveConfirmed(context.Context, objectstore.Object) error
}
type ArtifactCleaner struct {
	Repository ArtifactGCRepository
	Objects    ArtifactGCObjects
	Bucket     string
}

func (c ArtifactCleaner) CleanOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil { return false, err }
	if c.Repository == nil || c.Objects == nil || c.Bucket == "" {
		return false, errors.New("invalid artifact GC configuration")
	}
	recoverCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	recovered, err := c.Repository.Recover(recoverCtx)
	recoverErr := recoverCtx.Err()
	cancel()
	if err != nil {
		return false, err
	}
	if recoverErr != nil { return recovered > 0, recoverErr }
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := c.Repository.Claim(claim)
	claimErr := claim.Err()
	cancel()
	if err == nil { err = claimErr }
	if err != nil || ticket == nil {
		return recovered > 0, err
	}
	if ticket.ArtifactID == uuid.Nil || ticket.Token == uuid.Nil || ticket.TenantID == uuid.Nil || ticket.Bucket != c.Bucket || ticket.Object.Key == "" || ticket.Object.VersionID == "" || ticket.Object.VersionID == "null" || ticket.Object.Bytes < 1 {
		return false, errors.New("invalid artifact GC ticket")
	}
	if _, err = domain.ParseDigest(ticket.Object.SHA256); err != nil {
		return false, errors.New("invalid artifact GC digest")
	}
	remove, cancel := context.WithTimeout(ctx, 40*time.Second)
	err = c.Objects.RemoveConfirmed(remove, ticket.Object)
	removeErr := remove.Err()
	cancel()
	if err != nil {
		return false, err
	}
	// An adapter returning nil after timeout cannot authorize metadata deletion.
	if removeErr != nil { return recovered > 0, removeErr }
	finish, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := finish.Err(); err != nil { return recovered > 0, err }
	done, err := c.Repository.Finish(finish, *ticket)
	if err == nil { err = finish.Err() }
	return recovered > 0 || done, err
}

type SQLArtifactGC struct{ DB artifactjson.RowQuerier }

func (s SQLArtifactGC) Recover(ctx context.Context) (int, error) {
	if s.DB == nil {
		return 0, errors.New("artifact GC database required")
	}
	var n int
	err := s.DB.QueryRow(ctx, "SELECT photo.recover_artifact_gc()").Scan(&n)
	return n, err
}
func (s SQLArtifactGC) Claim(ctx context.Context) (*ArtifactGC, error) {
	if s.DB == nil {
		return nil, errors.New("artifact GC database required")
	}
	t := new(ArtifactGC)
	err := s.DB.QueryRow(ctx, "SELECT artifact_id,gc_token,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type FROM photo.claim_artifact_gc()").Scan(&t.ArtifactID, &t.Token, &t.TenantID, &t.Bucket, &t.Object.Key, &t.Object.VersionID, &t.Object.SHA256, &t.Object.Bytes, &t.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}
func (s SQLArtifactGC) Finish(ctx context.Context, t ArtifactGC) (bool, error) {
	if s.DB == nil || t.ArtifactID == uuid.Nil || t.Token == uuid.Nil {
		return false, errors.New("invalid artifact GC completion")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, "SELECT photo.finish_artifact_gc($1,$2)", t.ArtifactID, t.Token).Scan(&ok)
	return ok, err
}
