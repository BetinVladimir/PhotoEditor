package workflow

import (
	"context"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// PublishDeliveryAssemblyParams freezes the root reference outside admission TX.
// Dedicated SQL repeats current root/closure authority before and after object IO.
func PublishDeliveryAssemblyParams(ctx context.Context, db artifactjson.RowQuerier, objects artifactjson.Objects, bucket string, lease delivery.ExpansionLease, plan delivery.PreparePlan, assembly v2input.Ref) (v2input.Ref, []byte, error) {
	if _, err := NewDeliveryShardPublisher(db, objects, bucket, lease, plan); err != nil {
		return v2input.Ref{}, nil, err
	}
	decoded, decodeErr := hex.DecodeString(assembly.SHA256)
	if assembly.ArtifactID == uuid.Nil || decodeErr != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != assembly.SHA256 {
		return v2input.Ref{}, nil, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(v2input.DeliveryAssembleParams{SchemaVersion: 1, Assembly: assembly})
	if err != nil {
		return v2input.Ref{}, nil, err
	}
	owner, err := v2input.TaskID(lease.Workflow, "v2.delivery.assemble", 0)
	if err != nil {
		return v2input.Ref{}, nil, err
	}
	repository := deliveryAssemblyParamsRepository{db: db, bucket: bucket, lease: lease, purpose: "task_params", owner: owner, body: body}
	ref, err := (artifactjson.Publisher{Repository: repository, Objects: objects}).Publish(ctx, repository.scope(), body)
	if err != nil {
		return v2input.Ref{}, nil, err
	}
	return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, append([]byte(nil), body...), nil
}

type deliveryAssemblyParamsRepository struct {
	db      artifactjson.RowQuerier
	bucket  string
	lease   delivery.ExpansionLease
	purpose string
	owner   uuid.UUID
	body    []byte
}

func (r deliveryAssemblyParamsRepository) scope() artifactjson.Scope {
	return artifactjson.Scope{Tenant: r.lease.Tenant, Run: r.lease.Run, Owner: r.owner, Purpose: r.purpose, AssemblyLease: r.lease.Token}
}
func (r deliveryAssemblyParamsRepository) valid(scope artifactjson.Scope) bool {
	return r.db != nil && scope == r.scope() && scope.Tenant != uuid.Nil && scope.Run != uuid.Nil && r.owner != uuid.Nil && r.lease.Expansion != uuid.Nil && r.lease.Token != uuid.Nil && r.lease.Cursor == r.lease.SlotCount && r.lease.SlotCount >= 1 && r.lease.SlotCount <= 80000 && r.purpose == "task_params" && len(r.body) >= 2 && len(r.body) <= 65536 && len(r.bucket) >= 3 && len(r.bucket) <= 63
}
func (r deliveryAssemblyParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return artifactjson.Reservation{}, err
	}
	if !r.valid(scope) || sha != renderDerivativeBytesSHA(r.body) || size != int64(len(r.body)) {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err := r.db.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_assembly_params($1,$2,$3,$4,$5,$6,$7)`, r.lease.Expansion, r.lease.Token, string(r.body), r.bucket, scope.Tenant, scope.Run, scope.Owner).Scan(&outcome, &id, &token, &ready)
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
func (r deliveryAssemblyParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	if err := ctx.Err(); err != nil {
		return artifactjson.Ref{}, err
	}
	if !r.valid(scope) || ticket.ArtifactID == uuid.Nil || ticket.Token == uuid.Nil || object.Bytes != int64(len(r.body)) || object.SHA256 != renderDerivativeBytesSHA(r.body) || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return artifactjson.Ref{}, objectstore.ErrInvalid
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var accepted bool
	err = r.db.QueryRow(ctx, `SELECT photo.complete_v30_delivery_assembly_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.lease.Expansion, r.lease.Token, string(r.body), r.bucket, ticket.ArtifactID, ticket.Token, object.VersionID, scope.Tenant, scope.Run, scope.Owner).Scan(&accepted)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !accepted {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}
