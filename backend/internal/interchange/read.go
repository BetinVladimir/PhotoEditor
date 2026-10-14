package interchange

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
)

func ReadJob(ctx context.Context, tx ProofTransaction, who auth.Identity, tid, id uuid.UUID) (json.RawMessage, error) {
	return readJobProjection(ctx, tx, who, tid, id, false)
}
func readJobProjection(ctx context.Context, tx ProofTransaction, who auth.Identity, tid, id uuid.UUID, inspection bool) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tid == uuid.Nil || id == uuid.Nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	var raw []byte
	statement := `SELECT photo.get_v30_interchange($1,$2,$3,$4)`
	if inspection {
		statement = `SELECT photo.inspect_v30_interchange($1,$2,$3,$4)`
	}
	if err := tx.QueryRow(ctx, statement, tid, who.UserID, who.SessionID, id).Scan(&raw); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	if !json.Valid(raw) || inspection && !validInterchangeInspection(raw, tid, id) || !inspection && !validInterchangeResource(raw, tid, id) {
		return nil, domain.NewError("INTERCHANGE_DOCUMENT_INVALID", 500)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), raw...), nil
}
func (s Service) Get(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (json.RawMessage, error) {
	if s.DB == nil {
		return nil, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	var result json.RawMessage
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		result, err = ReadJob(ctx, tx, who, tid, id)
		return err
	})
	return result, err
}

func (s Service) Inspect(ctx context.Context, who auth.Identity, tid, id uuid.UUID) (json.RawMessage, error) {
	if s.DB == nil {
		return nil, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	var result json.RawMessage
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		result, err = readJobProjection(ctx, tx, who, tid, id, true)
		return err
	})
	return result, err
}
