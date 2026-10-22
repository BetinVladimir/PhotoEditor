package v2qualification

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"time"
)

type ReportCleanupObjects interface {
	RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error)
	RemoveConfirmed(context.Context, objectstore.Object) error
}
type ReportCleanupTicket struct {
	Artifact, Tenant, Project, Token uuid.UUID
	WriterState, Key, SHA, Bucket    string
	Version                          *string
	Bytes                            int64
}
type ReportCleanupRepository interface {
	Claim(context.Context) (*ReportCleanupTicket, error)
	CloseWriter(context.Context, ReportCleanupTicket, string) error
	Finish(context.Context, ReportCleanupTicket) error
}
type SQLReportCleanupRepository struct{ DB *pgxpool.Pool }

func (r SQLReportCleanupRepository) Claim(ctx context.Context) (*ReportCleanupTicket, error) {
	var t ReportCleanupTicket
	err := r.DB.QueryRow(ctx, `SELECT artifact,tenant,project,token,writer_state,object_key,object_version,sha,bytes,bucket FROM photo.claim_qualification_report_cleanup()`).Scan(&t.Artifact, &t.Tenant, &t.Project, &t.Token, &t.WriterState, &t.Key, &t.Version, &t.SHA, &t.Bytes, &t.Bucket)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
func (r SQLReportCleanupRepository) CloseWriter(ctx context.Context, t ReportCleanupTicket, v string) error {
	var ok bool
	if err := r.DB.QueryRow(ctx, `SELECT photo.close_qualification_report_cleanup_writer($1,$2,$3)`, t.Artifact, t.Token, v).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrUnqualified
	}
	return nil
}
func (r SQLReportCleanupRepository) Finish(ctx context.Context, t ReportCleanupTicket) error {
	var ok bool
	if err := r.DB.QueryRow(ctx, `SELECT photo.finish_qualification_report_cleanup($1,$2,$3,$4)`, t.Tenant, t.Artifact, t.Token, t.Version).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrUnqualified
	}
	return nil
}

type ReportPublicationCleanup struct {
	DB         *pgxpool.Pool
	Repository ReportCleanupRepository
	Objects    ReportCleanupObjects
	Bucket     string
}

func (c ReportPublicationCleanup) ProcessOne(ctx context.Context) (bool, error) {
	if (c.DB == nil && c.Repository == nil) || c.Objects == nil {
		return false, ErrUnqualified
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	repository := c.Repository
	if repository == nil {
		repository = SQLReportCleanupRepository{DB: c.DB}
	}
	t, err := repository.Claim(ctx)
	if err != nil || t == nil {
		return false, err
	}
	scope := ReportPublicationScope{Tenant: t.Tenant, Project: t.Project}
	if t.Artifact == uuid.Nil || t.Token == uuid.Nil || t.Bucket != c.Bucket || t.Key != reportPublicationKey(scope, t.Artifact) || t.Bytes < 2 || t.Bytes > 8*1024*1024 {
		return false, objectstore.ErrIntegrity
	}
	switch t.WriterState {
	case "idle":
		if t.Version != nil {
			return false, objectstore.ErrIntegrity
		} // Claim fenced Start; no writer ever existed.
	case "open":
		object, err := c.Objects.RecoverImmutable(ctx, t.Key, t.Bytes, t.SHA, "application/json")
		// Missing key cannot prove an outstanding remote PUT will not finish later.
		// Quota stays reserved; this ticket retries after the durable5minute fair backoff.
		if err != nil {
			return false, err
		}
		if !validPublishedReport(object, t.Key, t.SHA, t.Bytes) {
			return false, objectstore.ErrIntegrity
		}
		if err = repository.CloseWriter(ctx, *t, object.VersionID); err != nil {
			return false, err
		}
		v := object.VersionID
		t.Version = &v
		fallthrough
	case "closed":
		if t.Version == nil {
			return false, objectstore.ErrIntegrity
		}
		object := objectstore.Object{Key: t.Key, VersionID: *t.Version, SHA256: t.SHA, Bytes: t.Bytes, ContentType: "application/json"}
		if !validPublishedReport(object, t.Key, t.SHA, t.Bytes) {
			return false, objectstore.ErrIntegrity
		}
		if err = c.Objects.RemoveConfirmed(ctx, object); err != nil {
			return false, err
		}
	default:
		return false, objectstore.ErrIntegrity
	}
	if err = repository.Finish(ctx, *t); err != nil {
		return false, err
	}
	return true, nil
}

// Mounted in maintenance round-robin as well as the explicit operator CLI.
func (c ReportPublicationCleanup) CleanOne(ctx context.Context) (bool, error) {
	return c.ProcessOne(ctx)
}
func (c ReportPublicationCleanup) CleanupCycleTimeout() time.Duration { return 30 * time.Second }
