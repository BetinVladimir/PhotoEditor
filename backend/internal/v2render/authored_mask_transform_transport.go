package v2render

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
)

// The exact current-source locks remain held across EACH generic repository
// reserve/complete call. Real immutable IO occurs between those transactions.
type authoredMaskTransformTransportRepository struct {
	base authoredMaskTransformParamsRepository
}

func (r authoredMaskTransformTransportRepository) check(ctx context.Context, tx pgx.Tx, scope artifactjson.Scope) error {
	if scope.Purpose != "v2_input_manifest" {
		const prefix = "v2_input_chunk_"
		if !strings.HasPrefix(scope.Purpose, prefix) {
			return v2input.ErrInvalid
		}
		suffix := strings.TrimPrefix(scope.Purpose, prefix)
		ordinal, err := strconv.Atoi(suffix)
		if err != nil || ordinal < 0 || ordinal > 1953 || strconv.Itoa(ordinal) != suffix {
			return v2input.ErrInvalid
		}
	}
	if scope.Tenant != r.base.scope.Tenant || scope.Run != r.base.scope.Run || scope.Owner != r.base.scope.Owner {
		return v2input.ErrInvalid
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.lock_authored_render_mask_transform_transport_scope($1,$2,$3,$4)", scope.Tenant, r.base.parent, r.base.ordinal, scope.Owner).Scan(&raw); err != nil {
		return err
	}
	var s struct {
		Run  string `json:"run_id"`
		Task string `json:"task_id"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil || s.Run != scope.Run.String() || s.Task != scope.Owner.String() {
		return v2input.ErrExpired
	}
	return nil
}
func (r authoredMaskTransformTransportRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var out artifactjson.Reservation
	err := r.base.transaction(ctx, func(tx pgx.Tx) error {
		if err := r.check(ctx, tx, scope); err != nil {
			return err
		}
		var err error
		out, err = (artifactjson.SQLRepository{DB: tx, Bucket: r.base.bucket}).Reserve(ctx, scope, sha, size)
		return err
	})
	return out, err
}
func (r authoredMaskTransformTransportRepository) Complete(ctx context.Context, scope artifactjson.Scope, res artifactjson.Reservation, obj objectstore.Object) (artifactjson.Ref, error) {
	var out artifactjson.Ref
	err := r.base.transaction(ctx, func(tx pgx.Tx) error {
		if err := r.check(ctx, tx, scope); err != nil {
			return err
		}
		var err error
		out, err = (artifactjson.SQLRepository{DB: tx, Bucket: r.base.bucket}).Complete(ctx, scope, res, obj)
		return err
	})
	return out, err
}

func (p AuthoredMaskTransformPreparation) publishTransport(ctx context.Context, source AuthoredInstructionSource, plan AuthoredMaskTransformPlan) (v2input.PreparedSemanticTask, error) {
	var zero v2input.PreparedSemanticTask
	draft, err := plan.Frozen.DraftTask()
	if err != nil || draft.ID != plan.Task || p.Scheduler == nil || p.Params.Scheduler != p.Scheduler || p.Params.Objects == nil || p.Params.Bucket == "" {
		return zero, v2input.ErrInvalid
	}
	base := authoredMaskTransformParamsRepository{db: p.Scheduler, parent: source.Parent, ordinal: source.Instruction.Ordinal, bucket: p.Params.Bucket, scope: artifactjson.Scope{Tenant: source.Tenant, Run: plan.Run, Owner: plan.Task}}
	publisher := v2input.ScopedPublisher{Tenant: source.Tenant, Run: plan.Run, Documents: artifactjson.Publisher{Repository: authoredMaskTransformTransportRepository{base: base}, Objects: p.Params.Objects}}
	return v2input.PublishAuthoredMaskTransformTask(ctx, plan.Frozen, publisher)
}

var _ artifactjson.Repository = authoredMaskTransformTransportRepository{}
