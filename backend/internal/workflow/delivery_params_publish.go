package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// PublishDeliveryInitialParams uses independently committed SQL reservations and
// immutable PUT/recovery. Call outside every admission transaction. The document
// is parent-run scoped until the child shell exists; its owner is the child task.
func PublishDeliveryInitialParams(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, imageRaw []byte, targetICCSHA string, registry Registry) (v2input.Ref, error) {
	if db == nil || objects == nil || lease.ParentTask != plan.TaskID() || lease.Expansion == uuid.Nil || lease.Token == uuid.Nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	node, err := CompileDeliveryInitialNode(plan, lease.Run, child, imageRaw, targetICCSHA, registry)
	if err != nil {
		return v2input.Ref{}, err
	}
	repository := deliveryParamsRepository{db: db, bucket: bucket, lease: lease, slot: child.SlotIndex, owner: node.ID, params: append([]byte(nil), node.Params...)}
	publisher := artifactjson.Publisher{Repository: repository, Objects: objects}
	ref, err := publisher.Publish(ctx, repository.scope(), repository.params)
	if err != nil {
		return v2input.Ref{}, err
	}
	return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, nil
}

type deliveryParamsRepository struct {
	db     artifactjson.RowQuerier
	bucket string
	lease  delivery.ExpansionLease
	slot   int
	owner  uuid.UUID
	params []byte
}

func (r deliveryParamsRepository) scope() artifactjson.Scope {
	return artifactjson.Scope{Tenant: r.lease.Tenant, Run: r.lease.Run, Owner: r.owner, Purpose: "task_params", AssemblyLease: r.lease.Token}
}
func (r deliveryParamsRepository) valid(scope artifactjson.Scope) bool {
	return r.db != nil && scope == r.scope() && scope.Tenant != uuid.Nil && scope.Run != uuid.Nil && r.owner != uuid.Nil && r.lease.Expansion != uuid.Nil && r.lease.Token != uuid.Nil && r.slot >= 0 && r.slot < r.lease.SlotCount && r.slot <= r.lease.Cursor && len(r.params) >= 2 && len(r.params) <= 65536 && len(r.bucket) >= 3 && len(r.bucket) <= 63
}
func (r deliveryParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return artifactjson.Reservation{}, err
	}
	if !r.valid(scope) || sha != renderDerivativeBytesSHA(r.params) || size != int64(len(r.params)) {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err := r.db.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_child_params($1,$2,$3,$4,$5,$6,$7,$8)`, r.lease.Expansion, r.lease.Token, r.slot, string(r.params), r.bucket, scope.Tenant, scope.Run, scope.Owner).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	switch outcome {
	case "conflict":
		return artifactjson.Reservation{}, artifactjson.ErrConflict
	case "forbidden", "stale":
		return artifactjson.Reservation{}, artifactjson.ErrStale
	case "quota":
		return artifactjson.Reservation{}, artifactjson.ErrQuota
	case "created", "resumed", "replayed":
	default:
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	if id == nil || *id == uuid.Nil || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	return result, nil
}
func (r deliveryParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	if err := ctx.Err(); err != nil {
		return artifactjson.Ref{}, err
	}
	if !r.valid(scope) || ticket.ArtifactID == uuid.Nil || ticket.Token == uuid.Nil || object.Bytes != int64(len(r.params)) || object.SHA256 != renderDerivativeBytesSHA(r.params) || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return artifactjson.Ref{}, objectstore.ErrInvalid
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var accepted bool
	err = r.db.QueryRow(ctx, `SELECT photo.complete_v30_delivery_child_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, r.lease.Expansion, r.lease.Token, r.slot, string(r.params), r.bucket, ticket.ArtifactID, ticket.Token, object.VersionID, scope.Tenant, scope.Run, scope.Owner).Scan(&accepted)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !accepted {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}
