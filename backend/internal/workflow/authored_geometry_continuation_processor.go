package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"time"
)

// AuthoredGeometryContinuationProcessor appends only an already independently
// adopted geometry child. It does not create/qualify/dispatch a native proposal.
type AuthoredGeometryContinuationProcessor struct {
	DB          *pgxpool.Pool
	Reader      v2render.AuthoredGeometryProducerReader
	tenants     []uuid.UUID
	tenantIndex int
	after       map[uuid.UUID]uuid.UUID
}

func (p *AuthoredGeometryContinuationProcessor) ProcessOne(ctx context.Context) (bool, error) {
	if e := ctx.Err(); e != nil {
		return false, e
	}
	if p == nil || p.DB == nil || p.Reader.Scheduler != p.DB || p.Reader.Objects == nil || p.Reader.Bucket == "" || len(p.tenants) == 0 {
		return false, v2input.ErrInvalid
	}
	tenant := p.tenants[p.tenantIndex]
	p.tenantIndex = (p.tenantIndex + 1) % len(p.tenants)
	work, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var parent uuid.UUID
	var ordinal int
	e := database.TenantTransaction(work, p.DB, tenant, func(tx pgx.Tx) error {
		var cursor any
		if id := p.after[tenant]; id != uuid.Nil {
			cursor = id
		}
		return tx.QueryRow(work, `SELECT parent_id,ordinal FROM photo.next_authored_render_geometry_continuation($1,$2)`, tenant, cursor).Scan(&parent, &ordinal)
	})
	if errors.Is(e, pgx.ErrNoRows) {
		delete(p.after, tenant)
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if parent == uuid.Nil || ordinal < 1 || ordinal > 4998 || (p.after[tenant] != uuid.Nil && parent.String() <= p.after[tenant].String()) {
		return false, v2input.ErrInvalid
	}
	// Advance fairness before current checks: withdrawn candidate cannot starve later parents.
	p.after[tenant] = parent
	source, current, e := v2render.ReadAuthoredInstructionSource(work, p.DB, tenant, parent, ordinal)
	if e != nil {
		return false, e
	}
	if !current || source.Instruction.Kind != "geometry" {
		return false, nil
	}
	done, e := p.Reader.CommitCanonical(work, source)
	if errors.Is(e, v2render.ErrAuthoredGeometryPending) || errors.Is(e, v2input.ErrExpired) {
		return false, nil
	}
	return done, e
}
