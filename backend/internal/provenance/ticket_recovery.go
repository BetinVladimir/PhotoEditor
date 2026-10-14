package provenance

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// RecoverSigningTicket returns the original creation acknowledgement, not the
// current run status or proof of successful signing. No native/operator I/O.
func RecoverSigningTicket(ctx context.Context, tx SigningAdmissionTransaction, who auth.Identity, tid, export, policy uuid.UUID, key string) (v2input.JobTicket, bool, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tid == uuid.Nil || export == uuid.Nil || policy == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, false, ErrInvalid
	}
	request := struct {
		Export uuid.UUID `json:"export_id"`
		Policy uuid.UUID `json:"signing_policy_id"`
	}{export, policy}
	hash, err := canonical.Hash(request)
	if err != nil {
		return zero, false, err
	}
	var outcome string
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT outcome,ticket FROM photo.recover_v31_signing_ticket($1,$2,$3,$4,$5,$6,$7)`, tid, who.UserID, who.SessionID, export, policy, key, hash).Scan(&outcome, &raw); err != nil {
		return zero, false, err
	}
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	switch outcome {
	case "missing", "pending":
		if len(raw) != 0 {
			return zero, false, ErrInvalid
		}
		return zero, false, nil
	case "conflict":
		return zero, false, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "expired":
		return zero, false, domain.NewError("IDEMPOTENCY_EXPIRED", 410)
	case "forbidden":
		return zero, false, domain.NewError("NOT_FOUND", 404)
	case "replayed":
	default:
		return zero, false, ErrInvalid
	}
	if len(raw) < 2 || len(raw) > 4096 {
		return zero, false, ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return zero, false, ErrInvalid
	}
	var ticket v2input.JobTicket
	if decode(normalized, &ticket, 4096) != nil {
		return zero, false, ErrInvalid
	}
	workflow := uuid.NewSHA1(tid, []byte("provenance:"+who.UserID.String()+":"+key))
	if ticket.WorkflowID != workflow || ticket.RunID != uuid.NewSHA1(workflow, []byte("run")) || ticket.State != "queued" {
		return zero, false, ErrInvalid
	}
	return ticket, true, nil
}

type SQLSigningTicketReader struct{ DB *pgxpool.Pool }

func (s SQLSigningTicketReader) Read(ctx context.Context, who auth.Identity, tid, export, policy uuid.UUID, key string) (v2input.JobTicket, bool, error) {
	var ticket v2input.JobTicket
	var found bool
	if s.DB == nil {
		return ticket, false, ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		ticket, found, err = RecoverSigningTicket(ctx, tx, who, tid, export, policy, key)
		return err
	})
	if err != nil {
		return v2input.JobTicket{}, false, err
	}
	return ticket, found, nil
}
