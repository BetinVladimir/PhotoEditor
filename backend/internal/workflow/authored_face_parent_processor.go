package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"time"
)

// One bounded source per turn; cursor advances before physical IO so a revoked
// parent or missing model never starves later parents or configured tenants.
type AuthoredFaceParentProcessor struct {
	DB          *pgxpool.Pool
	Preparation *AuthoredFaceParentPreparation
	tenants     []uuid.UUID
	tenantIndex int
	after       map[uuid.UUID]uuid.UUID
}

func (p *AuthoredFaceParentProcessor) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p == nil || p.DB == nil || p.Preparation == nil || len(p.tenants) == 0 {
		return false, ErrAuthoredFaceUnavailable
	}
	tenant := p.tenants[p.tenantIndex]
	p.tenantIndex = (p.tenantIndex + 1) % len(p.tenants)
	work, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var parent uuid.UUID
	var ordinal int
	err := database.TenantTransaction(work, p.DB, tenant, func(tx pgx.Tx) error {
		var cursor any
		if id := p.after[tenant]; id != uuid.Nil {
			cursor = id
		}
		return tx.QueryRow(work, "SELECT parent_id,ordinal FROM photo.next_authored_render_face_preparation($1,$2)", tenant, cursor).Scan(&parent, &ordinal)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		delete(p.after, tenant)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if parent == uuid.Nil || ordinal < 1 || ordinal > 4998 || p.after[tenant] != uuid.Nil && parent.String() <= p.after[tenant].String() {
		return false, v2input.ErrInvalid
	}
	p.after[tenant] = parent
	progress, err := p.Preparation.Resume(work, tenant, parent, ordinal)
	if err != nil {
		return false, err
	}
	if err = work.Err(); err != nil {
		return false, err
	}
	return progress.Advanced, nil
}
