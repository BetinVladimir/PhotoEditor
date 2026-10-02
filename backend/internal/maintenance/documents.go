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

type DocumentCleanup struct {
	ID, Token, TenantID, RunID, ArtifactID uuid.UUID
	Bucket                                 string
}
type DocumentCleanupRepository interface {
	Claim(context.Context) (*DocumentCleanup, error)
	Finish(context.Context, DocumentCleanup) (bool, error)
}
type DocumentObjects interface {
	PurgeDocument(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
}
type DocumentCleaner struct {
	Repository DocumentCleanupRepository
	Objects    DocumentObjects
	Bucket     string
}

func (c DocumentCleaner) CleanOne(ctx context.Context) (bool, error) {
	if c.Repository == nil || c.Objects == nil || c.Bucket == "" {
		return false, errors.New("invalid document cleanup configuration")
	}
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := c.Repository.Claim(claim)
	cancel()
	if err != nil || ticket == nil {
		return false, err
	}
	if _, err = objectstore.DocumentKey(ticket.TenantID, ticket.RunID, ticket.ArtifactID); err != nil || ticket.ID == uuid.Nil || ticket.Token == uuid.Nil || ticket.Bucket != c.Bucket {
		return false, errors.New("invalid document cleanup ticket")
	}
	purge, cancel := context.WithTimeout(ctx, 40*time.Second)
	err = c.Objects.PurgeDocument(purge, ticket.TenantID, ticket.RunID, ticket.ArtifactID)
	cancel()
	if err != nil {
		return false, err
	}
	finish, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.Repository.Finish(finish, *ticket)
}

type SQLDocumentCleanup struct{ DB *pgxpool.Pool }

func (s SQLDocumentCleanup) Claim(ctx context.Context) (*DocumentCleanup, error) {
	ticket := new(DocumentCleanup)
	err := s.DB.QueryRow(ctx, "SELECT document_id,cleanup_token,tenant_id,run_id,artifact_id,bucket FROM photo.claim_document_cleanup()").Scan(&ticket.ID, &ticket.Token, &ticket.TenantID, &ticket.RunID, &ticket.ArtifactID, &ticket.Bucket)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ticket, nil
}
func (s SQLDocumentCleanup) Finish(ctx context.Context, ticket DocumentCleanup) (bool, error) {
	var done bool
	err := s.DB.QueryRow(ctx, "SELECT photo.finish_document_cleanup($1,$2)", ticket.ID, ticket.Token).Scan(&done)
	return done, err
}
