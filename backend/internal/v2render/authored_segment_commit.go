package v2render

import (
	"bytes"
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// CommitCanonical keeps technical child completion, immutable review=true and
// parent phase append distinct. The dedicated SQL helper must already be
// installed after actual per-kind task-current authority; absence is returned
// unchanged, never replaced by generic finalization or a completed-state write.
func (r AuthoredSegmentProducerReader) CommitCanonical(ctx context.Context, source AuthoredInstructionSource) (bool, error) {
	before, err := r.Read(ctx, source)
	if err != nil {
		return false, err
	}
	var completed bool
	err = database.TenantTransaction(ctx, r.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.complete_authored_render_phase_child($1,$2,$3)`, source.Tenant, source.Parent, before.task.RunID).Scan(&completed)
	})
	if err != nil {
		return false, err
	}
	if !completed {
		return false, ErrAuthoredSegmentPending
	}
	// Repeat all current SQL/physical observations after the committed technical
	// transition. No observation from before that mutation authorizes append.
	after, err := r.Read(ctx, source)
	if err != nil {
		return false, err
	}
	a, err := canonical.Marshal(before.task)
	if err != nil {
		return false, err
	}
	b, err := canonical.Marshal(after.task)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(a, b) || before.result != after.result || before.report != after.report || before.output != after.output {
		return false, v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	return (AuthoredPhaseStore{Scheduler: r.Scheduler}).RecordCanonical(ctx, source, after.task, after.output, after.result, after.report, nil, after.closure)
}
