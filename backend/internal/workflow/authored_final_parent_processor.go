package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"time"
)

// Bounded fair processor for actual compose/2 final phase and mandatory review settlement.
// Each poll admits or commits at most one final child; explicit review is never approved.
type AuthoredFinalParentProcessor struct {
	DB          *pgxpool.Pool
	Preparation *v2render.SQLAuthoredFinalContinuation
	tenants     []uuid.UUID
	tenantIndex int
	after       map[uuid.UUID]uuid.UUID
}

func (p *AuthoredFinalParentProcessor) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p == nil || p.DB == nil || p.Preparation == nil || len(p.tenants) == 0 {
		return false, v2input.ErrInvalid
	}
	tenant := p.tenants[p.tenantIndex]
	p.tenantIndex = (p.tenantIndex + 1) % len(p.tenants)
	work, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var parent uuid.UUID
	var ordinal int
	var committed bool
	err := database.TenantTransaction(work, p.DB, tenant, func(tx pgx.Tx) error {
		var cursor any
		if id := p.after[tenant]; id != uuid.Nil {
			cursor = id
		}
		return tx.QueryRow(work, "SELECT parent_id,ordinal,committed FROM photo.next_authored_render_final_preparation($1,$2)", tenant, cursor).Scan(&parent, &ordinal, &committed)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		delete(p.after, tenant)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if parent == uuid.Nil || ordinal < 1 || ordinal > 4999 || p.after[tenant] != uuid.Nil && parent.String() <= p.after[tenant].String() {
		return false, v2input.ErrInvalid
	}
	p.after[tenant] = parent
	// A committed final survives interruption between phase append and review
	// settlement. Re-enter only the protected settlement functions on replay.
	if committed {
		return (v2render.AuthoredPhaseStore{Scheduler: p.DB}).SettleReview(work, tenant, parent)
	}
	source, current, err := v2render.ReadAuthoredInstructionSource(work, p.DB, tenant, parent, ordinal)
	if err != nil {
		return false, err
	}
	if !current {
		return false, nil
	}
	done, err := p.Preparation.Resume(work, auth.Identity{UserID: source.Actor, SessionID: source.Session}, tenant, parent, ordinal)
	if errors.Is(err, v2render.ErrAuthoredFinalPending) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = work.Err(); err != nil {
		return false, err
	}
	return done, nil
}
