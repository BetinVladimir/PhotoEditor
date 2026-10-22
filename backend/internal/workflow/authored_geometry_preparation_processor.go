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

type AuthoredGeometryPreparationProcessor struct {
	DB          *pgxpool.Pool
	Preparation v2render.SQLAuthoredGeometryPreparation
	generations map[uuid.UUID]v2input.Ref
	tenants     []uuid.UUID
	tenantIndex int
	after       map[uuid.UUID]uuid.UUID
}

func (p *AuthoredGeometryPreparationProcessor) ProcessOne(ctx context.Context) (bool, error) {
	if e := ctx.Err(); e != nil {
		return false, e
	}
	if p == nil || p.DB == nil || p.Preparation.Scheduler != p.DB || len(p.tenants) == 0 {
		return false, v2input.ErrInvalid
	}
	tenant := p.tenants[p.tenantIndex]
	p.tenantIndex = (p.tenantIndex + 1) % len(p.tenants)
	work, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if p.Preparation.Native != nil {
		changed, e := v2render.ProcessAuthoredNativeLifecycle(work, p.DB, tenant)
		if e != nil || changed {
			return changed, e
		}
	}
	var parent uuid.UUID
	var ordinal int
	e := database.TenantTransaction(work, p.DB, tenant, func(tx pgx.Tx) error {
		var after any
		if p.after[tenant] != uuid.Nil {
			after = p.after[tenant]
		}
		return tx.QueryRow(work, `SELECT parent_id,ordinal FROM photo.next_authored_render_geometry_preparation($1,$2)`, tenant, after).Scan(&parent, &ordinal)
	})
	if errors.Is(e, pgx.ErrNoRows) {
		delete(p.after, tenant)
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if parent == uuid.Nil || ordinal < 1 || ordinal > 4998 || p.after[tenant] != uuid.Nil && parent.String() <= p.after[tenant].String() {
		return false, v2input.ErrInvalid
	}
	p.after[tenant] = parent // Withdrawn/pending first parent cannot monopolize the page.
	source, current, e := v2render.ReadAuthoredInstructionSource(work, p.DB, tenant, parent, ordinal)
	if e != nil {
		return false, e
	}
	if !current || source.Instruction.Kind != "geometry" {
		return false, nil
	}
	preparation := p.Preparation
	preparation.Generation = p.generations[tenant]
	_, e = preparation.Prepare(work, auth.Identity{UserID: source.Actor, SessionID: source.Session}, tenant, parent, ordinal)
	if errors.Is(e, v2render.ErrAuthoredGeometryPending) || errors.Is(e, v2input.ErrExpired) {
		return false, nil
	}
	return e == nil, e
}
