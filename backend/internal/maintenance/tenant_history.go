package maintenance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
)

// Appender must return only after independent history durably stores this exact
// event. Stable identity/time survives cleanup-token rotation and process retry.
// It is not satisfied by a queue publish or a restored-DB-only outbox receipt.
type TenantDeletionHistoryAppender interface {
	AppendTenantDeletionRequested(context.Context, uuid.UUID, uuid.UUID, time.Time) error
}
type TenantDeletionHistory interface {
	RecordRequested(context.Context, TenantDeletion) error
}
type SQLTenantDeletionHistory struct {
	DB       artifactjson.RowQuerier
	Appender TenantDeletionHistoryAppender
}

func (s SQLTenantDeletionHistory) RecordRequested(ctx context.Context, ticket TenantDeletion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || s.Appender == nil || ticket.JobID == uuid.Nil || ticket.Token == uuid.Nil || ticket.TenantID == uuid.Nil {
		return errors.New("tenant deletion history dependencies required")
	}
	read := func() (time.Time, error) {
		var at time.Time
		err := s.DB.QueryRow(ctx, "SELECT requested_at FROM photo.v32_tenant_deletion_history_intent($1,$2,$3)", ticket.JobID, ticket.Token, ticket.TenantID).Scan(&at)
		if err != nil {
			return time.Time{}, err
		}
		if at.IsZero() {
			return time.Time{}, errors.New("tenant deletion history intent unavailable")
		}
		return at.UTC(), nil
	}
	at, err := read()
	if err != nil {
		return err
	}
	event := uuid.NewSHA1(ticket.JobID, []byte("tenant-deletion-history:requested:"+ticket.TenantID.String()))
	if err = s.Appender.AppendTenantDeletionRequested(ctx, event, ticket.TenantID, at); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	current, err := read()
	if err != nil {
		return err
	}
	if !current.Equal(at) {
		return errors.New("tenant deletion history intent changed")
	}
	return ctx.Err()
}
