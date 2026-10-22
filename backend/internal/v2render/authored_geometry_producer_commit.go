package v2render

import (
	"bytes"
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// CommitCanonical never promotes a geometry child without its real physical adopter.
func (r AuthoredGeometryProducerReader) CommitCanonical(ctx context.Context, source AuthoredInstructionSource) (bool, error) {
	before, e := r.Read(ctx, source)
	if e != nil {
		return false, e
	}
	var completed bool
	e = database.TenantTransaction(ctx, r.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.complete_authored_render_phase_child($1,$2,$3)`, source.Tenant, source.Parent, before.task.RunID).Scan(&completed)
	})
	if e != nil {
		return false, e
	}
	if !completed {
		return false, ErrAuthoredGeometryPending
	}
	after, e := r.Read(ctx, source)
	if e != nil {
		return false, e
	}
	at, e := canonical.Marshal(before.task)
	if e != nil {
		return false, e
	}
	bt, e := canonical.Marshal(after.task)
	if e != nil {
		return false, e
	}
	an, e := canonical.Marshal(before.native)
	if e != nil {
		return false, e
	}
	bn, e := canonical.Marshal(after.native)
	if e != nil {
		return false, e
	}
	if !bytes.Equal(at, bt) || !bytes.Equal(an, bn) || before.result != after.result || before.report != after.report || before.output != after.output || !bytes.Equal(before.proof, after.proof) {
		return false, v2input.ErrExpired
	}
	if e = ctx.Err(); e != nil {
		return false, e
	}
	return (AuthoredPhaseStore{Scheduler: r.Scheduler}).RecordCanonical(ctx, source, after.task, after.output, after.result, after.report, &after.native, after.closure)
}
