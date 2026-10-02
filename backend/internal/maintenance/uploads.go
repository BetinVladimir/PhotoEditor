package maintenance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

type UploadAbortCleanup struct {
	ID, Token, UploadID, TenantID uuid.UUID
	ObjectKey, MultipartID        string
}

type UploadCleanupRepository interface {
	ExpireOne(context.Context) (bool, error)
	Claim(context.Context) (*UploadAbortCleanup, error)
	Finish(context.Context, UploadAbortCleanup) (bool, error)
}

type MultipartAborter interface {
	AbortMultipart(context.Context, string, string) error
}

type UploadCleaner struct {
	Repository UploadCleanupRepository
	Objects    MultipartAborter
}

// CleanOne first expires one stale session and then drains one durable abort.
// Both operations are bounded, so a large expiry backlog cannot starve cleanup.
func (c UploadCleaner) CleanOne(ctx context.Context) (bool, error) {
	if c.Repository == nil || c.Objects == nil {
		return false, errors.New("invalid upload cleanup configuration")
	}
	expire, cancel := context.WithTimeout(ctx, 5*time.Second)
	expired, err := c.Repository.ExpireOne(expire)
	cancel()
	if err != nil {
		return false, err
	}
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := c.Repository.Claim(claim)
	cancel()
	if err != nil || ticket == nil {
		return expired, err
	}
	if ticket.ID == uuid.Nil || ticket.Token == uuid.Nil || ticket.UploadID == uuid.Nil || ticket.TenantID == uuid.Nil || ticket.ObjectKey == "" || ticket.MultipartID == "" {
		return false, errors.New("invalid upload cleanup ticket")
	}
	abort, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = c.Objects.AbortMultipart(abort, ticket.ObjectKey, ticket.MultipartID)
	cancel()
	if err != nil && !errors.Is(err, objectstore.ErrMissing) {
		return false, err
	}
	finish, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	done, err := c.Repository.Finish(finish, *ticket)
	return expired || done, err
}

type SQLUploadCleanup struct{ DB artifactjson.RowQuerier }

func (s SQLUploadCleanup) ExpireOne(ctx context.Context) (bool, error) {
	if s.DB == nil {
		return false, errors.New("upload cleanup database required")
	}
	var expired bool
	err := s.DB.QueryRow(ctx, "SELECT photo.expire_upload_session()").Scan(&expired)
	return expired, err
}

func (s SQLUploadCleanup) Claim(ctx context.Context) (*UploadAbortCleanup, error) {
	if s.DB == nil {
		return nil, errors.New("upload cleanup database required")
	}
	t := new(UploadAbortCleanup)
	err := s.DB.QueryRow(ctx, "SELECT cleanup_id,cleanup_token,upload_id,tenant_id,object_key,multipart_id FROM photo.claim_upload_abort_cleanup()").Scan(
		&t.ID, &t.Token, &t.UploadID, &t.TenantID, &t.ObjectKey, &t.MultipartID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s SQLUploadCleanup) Finish(ctx context.Context, t UploadAbortCleanup) (bool, error) {
	if s.DB == nil || t.ID == uuid.Nil || t.Token == uuid.Nil {
		return false, errors.New("invalid upload cleanup completion")
	}
	var done bool
	err := s.DB.QueryRow(ctx, "SELECT photo.finish_upload_abort_cleanup($1,$2)", t.ID, t.Token).Scan(&done)
	return done, err
}
