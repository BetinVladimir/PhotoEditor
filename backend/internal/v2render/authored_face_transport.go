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
type authoredFaceTransportRepository struct{ base authoredFaceParamsRepository }

func (r authoredFaceTransportRepository) check(ctx context.Context, tx pgx.Tx, scope artifactjson.Scope) error {
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
	if err := tx.QueryRow(ctx, "SELECT photo.lock_authored_render_face_transport_scope($1,$2,$3,$4,$5)", scope.Tenant, r.base.parent, r.base.ordinal, r.base.operation, scope.Owner).Scan(&raw); err != nil {
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
func (r authoredFaceTransportRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
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
func (r authoredFaceTransportRepository) Complete(ctx context.Context, scope artifactjson.Scope, res artifactjson.Reservation, obj objectstore.Object) (artifactjson.Ref, error) {
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
func (p AuthoredFacePreparation) PublishDetectorTransport(ctx context.Context, source AuthoredInstructionSource, frozen v2input.FrozenSemanticDetectorTask) (v2input.PreparedSemanticTask, error) {
	return p.publishNativeTransport(ctx, source, "face.detect", frozen)
}
func (p AuthoredFacePreparation) PublishLandmarkTransport(ctx context.Context, source AuthoredInstructionSource, frozen v2input.FrozenSemanticLandmarkTask) (v2input.PreparedSemanticTask, error) {
	return p.publishNativeTransport(ctx, source, "face.landmarks", frozen)
}
func (p AuthoredFacePreparation) PublishParserTransport(ctx context.Context, source AuthoredInstructionSource, frozen v2input.FrozenSemanticFaceParseTask) (v2input.PreparedSemanticTask, error) {
	return p.publishNativeTransport(ctx, source, "face.parse", frozen)
}
func (p AuthoredFacePreparation) publishNativeTransport(ctx context.Context, source AuthoredInstructionSource, operation string, frozen any) (v2input.PreparedSemanticTask, error) {
	empty := v2input.PreparedSemanticTask{}
	run, workflow, err := AuthoredFacePrerequisiteIDs(source, operation)
	if err != nil {
		return empty, err
	}
	task, err := v2input.TaskID(workflow, operation, 0)
	if err != nil {
		return empty, err
	}
	if p.Scheduler == nil || p.Params.Scheduler != p.Scheduler || p.Params.Objects == nil || p.Params.Bucket == "" {
		return empty, v2input.ErrInvalid
	}
	var draft v2input.DraftTask
	switch f := frozen.(type) {
	case v2input.FrozenSemanticDetectorTask:
		if operation != "face.detect" {
			return empty, v2input.ErrInvalid
		}
		draft, err = f.DraftTask()
	case v2input.FrozenSemanticLandmarkTask:
		if operation != "face.landmarks" {
			return empty, v2input.ErrInvalid
		}
		draft, err = f.DraftTask()
	case v2input.FrozenSemanticFaceParseTask:
		if operation != "face.parse" {
			return empty, v2input.ErrInvalid
		}
		draft, err = f.DraftTask()
	default:
		return empty, v2input.ErrInvalid
	}
	if err != nil || draft.ID != task {
		return empty, v2input.ErrInvalid
	}
	base := authoredFaceParamsRepository{db: p.Scheduler, parent: source.Parent, ordinal: source.Instruction.Ordinal, operation: operation, bucket: p.Params.Bucket, scope: artifactjson.Scope{Tenant: source.Tenant, Run: run, Owner: task}}
	publisher := v2input.ScopedPublisher{Tenant: source.Tenant, Run: run, Documents: artifactjson.Publisher{Repository: authoredFaceTransportRepository{base: base}, Objects: p.Params.Objects}}
	switch f := frozen.(type) {
	case v2input.FrozenSemanticDetectorTask:
		return v2input.PublishSemanticDetectorTask(ctx, f, publisher)
	case v2input.FrozenSemanticLandmarkTask:
		return v2input.PublishSemanticLandmarkTask(ctx, f, publisher)
	case v2input.FrozenSemanticFaceParseTask:
		return v2input.PublishSemanticFaceParseTask(ctx, f, publisher)
	}
	return empty, v2input.ErrInvalid
}

var _ artifactjson.Repository = authoredFaceTransportRepository{}
