package maintenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"time"
)

type OutputCleanup struct {
	UploadID, Token, TenantID, TaskID, ArtifactID uuid.UUID
	Attempt                                       int
	Fence                                         int64
	Bucket, Key                                   string
}
type OrphanObjects interface {
	PurgeAttempt(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, int64) error
}
type CleanupRepository interface {
	Claim(context.Context) (*OutputCleanup, error)
	Finish(context.Context, OutputCleanup) (bool, error)
}
type OutputCleaner struct {
	Repository CleanupRepository
	Objects    OrphanObjects
	Bucket     string
}

func (c OutputCleaner) CleanOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil { return false, err }
	if c.Repository == nil || c.Objects == nil || c.Bucket == "" {
		return false, errors.New("invalid cleanup configuration")
	}
	claim, done := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := c.Repository.Claim(claim)
	claimErr := claim.Err()
	done()
	if err == nil { err = claimErr }
	if err != nil || ticket == nil {
		return false, err
	}
	expected, err := objectstore.AttemptKey(ticket.TenantID, ticket.TaskID, ticket.ArtifactID, ticket.Attempt, ticket.Fence)
	if err != nil || ticket.UploadID == uuid.Nil || ticket.Token == uuid.Nil || ticket.Bucket != c.Bucket || ticket.Key != expected {
		return false, errors.New("invalid cleanup ticket")
	}
	purge, done := context.WithTimeout(ctx, 40*time.Second)
	err = c.Objects.PurgeAttempt(purge, ticket.TenantID, ticket.TaskID, ticket.ArtifactID, ticket.Attempt, ticket.Fence)
	purgeErr := purge.Err()
	done()
	if err != nil {
		return false, err
	}
	if purgeErr != nil { return false, purgeErr }
	finish, done := context.WithTimeout(ctx, 5*time.Second)
	defer done()
	if err := finish.Err(); err != nil { return false, err }
	accepted, err := c.Repository.Finish(finish, *ticket)
	if err == nil { err = finish.Err() }
	return accepted, err
}

type SQLCleanup struct{ DB *pgxpool.Pool }

func (s SQLCleanup) Claim(ctx context.Context) (*OutputCleanup, error) {
	var result *OutputCleanup
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		ticket := new(OutputCleanup)
		err := tx.QueryRow(ctx, `SELECT upload_id,cleanup_token,tenant_id,task_id,artifact_id,attempt,fence,bucket,object_key FROM photo.claim_output_cleanup()`).Scan(&ticket.UploadID, &ticket.Token, &ticket.TenantID, &ticket.TaskID, &ticket.ArtifactID, &ticket.Attempt, &ticket.Fence, &ticket.Bucket, &ticket.Key)
		if errors.Is(err, pgx.ErrNoRows) {
			result = nil
			return nil
		}
		if err == nil {
			result = ticket
		}
		return err
	})
	return result, err
}
func (s SQLCleanup) Finish(ctx context.Context, t OutputCleanup) (bool, error) {
	var result bool
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.finish_output_cleanup($1,$2)`, t.UploadID, t.Token).Scan(&result)
	})
	return result, err
}
