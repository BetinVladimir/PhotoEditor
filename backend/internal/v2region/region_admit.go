package v2region

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Admit joins the two existing database phases with external closure publication.
// Params and the frozen source snapshot already exist. A failed second phase
// leaves a blocked draft that the same immutable request can resume.
func (n FrozenRegionNode) Admit(ctx context.Context, db *pgxpool.Pool, publisher v2input.Publisher,
	who auth.Identity, key string, snapshot uuid.UUID, policySHA string, reservedBytes int64, requestSHA string) (v2input.JobTicket, error) {
	var empty v2input.JobTicket
	if db == nil || publisher == nil || !validRegionSHA(requestSHA) {
		return empty, ErrRegionInvalid
	}
	shell, sources, err := n.DraftShell(who, key, snapshot, policySHA, reservedBytes)
	if err != nil {
		return empty, err
	}
	var outcome string
	var run uuid.UUID
	err = database.TenantTransaction(ctx, db, shell.Tenant, func(tx pgx.Tx) error {
		var e error
		outcome, run, e = n.BeginInTransaction(ctx, tx, who, key, snapshot, policySHA, reservedBytes, requestSHA)
		return e
	})
	if err != nil {
		return empty, err
	}
	switch outcome {
	case "conflict":
		return empty, v2input.ErrConflict
	case "expired":
		return empty, v2input.ErrExpired
	case "created", "pending", "replayed":
		if run != shell.Run {
			return empty, ErrRegionInvalid
		}
	default:
		return empty, ErrRegionInvalid
	}
	if outcome != "replayed" {
		cursor, e := sources[0].Open(ctx)
		if e != nil {
			return empty, e
		}
		if cursor == nil {
			return empty, ErrRegionInvalid
		}
		built, buildErr := v2input.BuildInputClosure(ctx, sources[0].ID, cursor, publisher)
		if closer, ok := cursor.(io.Closer); ok {
			_ = closer.Close()
		}
		if buildErr != nil {
			return empty, buildErr
		}
		err = database.TenantTransaction(ctx, db, shell.Tenant, func(tx pgx.Tx) error {
			return n.SealInTransaction(ctx, tx, who, key, snapshot, policySHA, reservedBytes, built)
		})
		if err != nil {
			return empty, err
		}
	}
	ticket, err := (v2input.SQLAdmissionStore{DB: db}).Ticket(ctx, shell.Tenant, who.UserID, key, requestSHA)
	if err != nil {
		return empty, err
	}
	if ticket.RunID != shell.Run || ticket.WorkflowID != shell.WorkflowID || ticket.State != "queued" {
		return empty, ErrRegionInvalid
	}
	return ticket, nil
}
