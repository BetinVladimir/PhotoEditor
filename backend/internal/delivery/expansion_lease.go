package delivery

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"time"
)

type ExpansionLease struct {
	Expansion, Token                           uuid.UUID
	Until                                      time.Time
	Tenant, Run, Workflow, Package, ParentTask uuid.UUID
	Cursor, SlotCount                          int
}

type ExpansionLeaseStore struct {
	DB       ProofTransaction
	Instance uuid.UUID
	Bucket   string
}

func (s ExpansionLeaseStore) Claim(ctx context.Context) (*ExpansionLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Instance == uuid.Nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return nil, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	var lease ExpansionLease
	err := s.DB.QueryRow(ctx, `SELECT * FROM photo.claim_v30_delivery_expansion_bound($1,$2)`, s.Instance, s.Bucket).Scan(
		&lease.Expansion, &lease.Token, &lease.Until, &lease.Tenant, &lease.Run, &lease.Workflow, &lease.Package, &lease.ParentTask, &lease.Cursor, &lease.SlotCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !lease.valid() {
		return nil, domain.NewError("DELIVERY_LEASE_INVALID", 500)
	}
	return &lease, nil
}

// Valid reports whether the lease carries a complete, bounded identity.
// Current ownership and expiry still require SQL authorization.
func (l ExpansionLease) Valid() bool { return l.valid() }

func (l ExpansionLease) valid() bool {
	return l.Expansion != uuid.Nil && l.Token != uuid.Nil && !l.Until.IsZero() && l.Tenant != uuid.Nil && l.Run != uuid.Nil && l.Workflow != uuid.Nil && l.Package != uuid.Nil && l.ParentTask != uuid.Nil && l.SlotCount >= 1 && l.SlotCount <= 80000 && l.Cursor >= 0 && l.Cursor <= l.SlotCount
}
func (s ExpansionLeaseStore) Renew(ctx context.Context, l ExpansionLease) error {
	return s.update(ctx, l, false)
}
func (s ExpansionLeaseStore) Release(ctx context.Context, l ExpansionLease) error {
	return s.update(ctx, l, true)
}
func (s ExpansionLeaseStore) update(ctx context.Context, l ExpansionLease, release bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil {
		return domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	if !l.valid() {
		return domain.NewError("DELIVERY_LEASE_INVALID", 422)
	}
	var accepted bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.update_v30_delivery_expansion_lease($1,$2,$3)`, l.Expansion, l.Token, release).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_LEASE_STALE", 409)
	}
	return nil
}
