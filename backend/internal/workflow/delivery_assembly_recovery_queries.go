package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

// Recovery is bound to one immutable assembly root as well as the current lease.
// SQL independently reauthorizes every shard and its terminal export producers.
type deliveryAssemblyRecoveryQueries struct {
	deliveryAssemblyObservationQueries
	root v2input.Ref
}
type deliveryAssemblyRecoveryRow struct {
	owner deliveryAssemblyRecoveryQueries
	ctx   context.Context
	query string
	args  []any
}

func (q deliveryAssemblyRecoveryQueries) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return deliveryAssemblyRecoveryRow{q, ctx, query, append([]any(nil), args...)}
}
func (r deliveryAssemblyRecoveryRow) Scan(dest ...any) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	q := r.owner
	l := q.lease
	if q.db == nil || !l.Valid() || q.root.ArtifactID == uuid.Nil || domainDigestInvalid(q.root.SHA256) {
		return v2input.ErrInvalid
	}
	switch r.query {
	case `SELECT photo.update_v30_delivery_expansion_lease($1,$2,$3)`:
		return q.deliveryAssemblyObservationQueries.QueryRow(r.ctx, r.query, r.args...).Scan(dest...)
	case `SELECT photo.v30_delivery_saved_assembly($1,$2,$3,$4)`,
		`SELECT photo.v30_delivery_assembly_inventory($1,$2,$3,$4)`:
		if len(r.args) != 4 {
			return v2input.ErrInvalid
		}
	case `SELECT photo.v30_delivery_saved_shard($1,$2,$3,$4,$5)`:
		if len(r.args) != 5 {
			return v2input.ErrInvalid
		}
		index, ok := r.args[4].(int)
		if !ok || index < 0 || index >= (l.SlotCount+499)/500 {
			return v2input.ErrInvalid
		}
	default:
		return v2input.ErrInvalid
	}
	expansion, ok := r.args[0].(uuid.UUID)
	token, good := r.args[1].(uuid.UUID)
	root, rootOK := r.args[2].(uuid.UUID)
	sha, shaOK := r.args[3].(string)
	if !ok || !good || !rootOK || !shaOK || expansion != l.Expansion || token != l.Token || root != q.root.ArtifactID || sha != q.root.SHA256 {
		return v2input.ErrInvalid
	}
	return assemblyTenantTransaction(r.ctx, q.db, l, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, r.query, r.args...).Scan(dest...)
	})
}
