package semanticreview

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RegisterApprovedExposureInputsInTransaction independently recompiles the
// published private review evidence before registering and sealing input rows.
// The caller owns the final admission transaction and MUST roll it back on any
// error. It must already have authenticated scope and created the exact draft
// child, review batch and queued target bindings. This does not queue the run;
// source-byte authority, activation, measured owner costs and quota must still
// be checked by that caller before committing admission.
func RegisterApprovedExposureInputsInTransaction(ctx context.Context, tx pgx.Tx,
	tenant, project, actor, child uuid.UUID, prepared PreparedReview, targets []LockedTarget,
	request ApplyDecision, decision PublishedDecision, inputs []ExposureTaskInputs,
	published []PreparedExposureTask) (ExposureCommitInputs, error) {
	empty := ExposureCommitInputs{}
	if tx == nil || project == uuid.Nil {
		return empty, ErrInvalid
	}
	verified, err := PrepareApprovedExposureCommitInputs(ctx, tenant, actor, child, prepared, targets, request, decision, inputs, published)
	if err != nil {
		return empty, err
	}
	return registerVerifiedExposureInputs(ctx, tx, tenant, project, prepared.Sources.RunID, child, verified)
}

// Only compiler-owned, reverified projections may reach this internal helper.
func registerVerifiedExposureInputs(ctx context.Context, tx pgx.Tx, tenant, project, parent, child uuid.UUID, verified ExposureCommitInputs) (ExposureCommitInputs, error) {
	empty := ExposureCommitInputs{}
	expectedRows := 0
	for _, graph := range verified.Graphs {
		expectedRows += graph.Built.Count + len(graph.Built.Chunks) + 1
	}
	var registered int
	if err := tx.QueryRow(ctx, "SELECT photo.register_semantic_apply_inputs($1,$2,$3,$4,$5,$6,$7)",
		tenant, project, parent, child, verified.Nodes, verified.Transport, verified.Inventory).Scan(&registered); err != nil {
		return empty, err
	}
	if registered != expectedRows {
		return empty, fmt.Errorf("%w: incomplete Apply inventory registration", ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var sealed int
	if err := tx.QueryRow(ctx, "SELECT photo.seal_semantic_apply_inputs($1,$2,$3,$4,$5,$6,$7,$8)",
		tenant, project, parent, child, json.RawMessage(verified.Review.Params), verified.Nodes, verified.Transport, verified.Inventory).Scan(&sealed); err != nil {
		return empty, err
	}
	if sealed != len(verified.Graphs) {
		return empty, fmt.Errorf("%w: incomplete Apply input seal", ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return verified, nil
}
