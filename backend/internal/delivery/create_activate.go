package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

func (i CreateIntent) ActivateCreate(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID) (idempotency.Response, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Response{}, err
	}
	if tx == nil || tid != i.tenant || who.UserID != i.actor || who.SessionID == uuid.Nil || i.identity.Run == uuid.Nil {
		return idempotency.Response{}, v2input.ErrInvalid
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT photo.activate_v30_delivery_create($1,$2,$3,$4,$5)`, tid, who.UserID, who.SessionID, i.key, i.requestSHA).Scan(&raw); err != nil {
		// Only this explicit atomic seal refusal is a business conflict.
		// Unrelated integrity/database errors must retain their original cause.
		var refusal *pgconn.PgError
		if errors.As(err, &refusal) && refusal.Code == "23514" && refusal.Message == "DELIVERY_INITIAL_ACTIVATION_REFUSED" {
			return idempotency.Response{}, domain.NewError("DELIVERY_CREATE_STALE", 409)
		}
		return idempotency.Response{}, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return idempotency.Response{}, domain.NewError("DELIVERY_CREATE_STALE", 409)
	}
	if len(raw) > 4096 {
		return idempotency.Response{}, v2input.ErrInvalid
	}
	var receipt struct {
		Ticket   v2input.JobTicket `json:"ticket"`
		Replayed *bool             `json:"replayed"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&receipt) != nil || decoder.Decode(new(any)) != io.EOF || receipt.Replayed == nil || receipt.Ticket.RunID != i.identity.Run || receipt.Ticket.WorkflowID != i.identity.Workflow || receipt.Ticket.State != "queued" {
		return idempotency.Response{}, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(receipt.Ticket)
	if err != nil {
		return idempotency.Response{}, err
	}
	id := i.identity.Package
	return idempotency.Response{Status: 202, Body: body, ResourceID: &id, Replayed: *receipt.Replayed}, nil
}

// CreateDelivery replays completed tickets before requiring imaging/storage
// configuration. Fresh requests prepare all phases then atomically activate
// and save the ticket. API routing/configuration must supply the concrete ports.
func (s Service) CreateDelivery(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, deps CreateDeliveryDependencies) (idempotency.Response, error) {
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return idempotency.Response{}, err
	}
	request = intent.Request()
	return coordinateCreateDelivery(ctx, createDeliveryPhases{
		recover: func(ctx context.Context) (CreateRecovery, error) {
			return s.RecoverSelectedCreate(ctx, who, tid, request, key)
		},
		prepare: func(ctx context.Context) error {
			_, err := s.PrepareSelectedDelivery(ctx, who, tid, request, key, deps)
			return err
		},
		activate: func(ctx context.Context) (idempotency.Response, error) {
			var out idempotency.Response
			err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
				var err error
				out, err = intent.ActivateCreate(ctx, tx, who, tid)
				return err
			})
			if err != nil {
				return idempotency.Response{}, err
			}
			return out, nil
		},
	})
}

// A concurrent request can complete between any two preparation phases. Only
// current authorized ledger recovery may turn a preparation error into replay;
// incomplete evidence must never manufacture a success ticket.
type createDeliveryPhases struct {
	recover  func(context.Context) (CreateRecovery, error)
	prepare  func(context.Context) error
	activate func(context.Context) (idempotency.Response, error)
}

func coordinateCreateDelivery(ctx context.Context, p createDeliveryPhases) (idempotency.Response, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Response{}, err
	}
	recovery, err := p.recover(ctx)
	if err != nil {
		return idempotency.Response{}, err
	}
	if !recovery.Completed {
		if preparationErr := p.prepare(ctx); preparationErr != nil {
			if err := ctx.Err(); err != nil {
				return idempotency.Response{}, err
			}
			current, recoveryErr := p.recover(ctx)
			if recoveryErr != nil {
				return idempotency.Response{}, recoveryErr
			}
			if !current.Completed {
				return idempotency.Response{}, preparationErr
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return idempotency.Response{}, err
	}
	return p.activate(ctx)
}
