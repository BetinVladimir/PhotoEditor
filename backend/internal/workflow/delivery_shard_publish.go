package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
)

// NewDeliveryShardPublisher binds immutable object publication to a complete
// parent admission lease. SQL revalidates exact slots at reserve and completion.
// Construct/call outside admission transactions; root assembly is not allowed.
func NewDeliveryShardPublisher(db artifactjson.RowQuerier, objects artifactjson.Objects, bucket string, lease delivery.ExpansionLease, plan delivery.PreparePlan) (delivery.PublishDocument, error) {
	workflow, err := v2input.WorkflowID(lease.Run)
	if err != nil || db == nil || objects == nil || lease.Workflow != workflow || lease.ParentTask != plan.TaskID() || lease.Cursor != lease.SlotCount || lease.SlotCount < 1 || lease.SlotCount > 80000 || lease.Token == uuid.Nil || lease.Expansion == uuid.Nil || lease.Tenant == uuid.Nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, v2input.ErrInvalid
	}
	owner, err := v2input.TaskID(workflow, "v2.delivery.assemble", 0)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, purpose string, body []byte) (v2input.Ref, error) {
		repository := deliveryShardRepository{db: db, bucket: bucket, lease: lease, purpose: purpose, owner: owner, body: append([]byte(nil), body...)}
		if !repository.valid(repository.scope()) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		ref, err := (artifactjson.Publisher{Repository: repository, Objects: objects}).Publish(ctx, repository.scope(), repository.body)
		if err != nil {
			return v2input.Ref{}, err
		}
		return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, nil
	}, nil
}
func validDeliveryShardPurpose(purpose string, slots int) bool {
	const prefix = "v30_delivery_slots_"
	if !strings.HasPrefix(purpose, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(purpose, prefix)
	ordinal, err := strconv.Atoi(suffix)
	return err == nil && ordinal >= 0 && ordinal < 160 && ordinal < (slots+499)/500 && suffix == fmt.Sprintf("%03d", ordinal)
}

type deliveryShardRepository struct {
	db            artifactjson.RowQuerier
	bucket        string
	lease         delivery.ExpansionLease
	purpose       string
	owner         uuid.UUID
	body          []byte
	assemblyProof []byte
}

func (r deliveryShardRepository) scope() artifactjson.Scope {
	return artifactjson.Scope{Tenant: r.lease.Tenant, Run: r.lease.Run, Owner: r.owner, Purpose: r.purpose, AssemblyLease: r.lease.Token}
}
func (r deliveryShardRepository) valid(scope artifactjson.Scope) bool {
	return r.db != nil && scope == r.scope() && scope.Tenant != uuid.Nil && scope.Run != uuid.Nil && r.owner != uuid.Nil && r.lease.Expansion != uuid.Nil && r.lease.Token != uuid.Nil && r.lease.Cursor == r.lease.SlotCount && r.lease.SlotCount >= 1 && r.lease.SlotCount <= 80000 && (validDeliveryShardPurpose(r.purpose, r.lease.SlotCount) && len(r.assemblyProof) == 0 || r.purpose == "v30_delivery_assembly" && len(r.assemblyProof) > 0) && len(r.body) >= 2 && len(r.body) <= 1<<20 && len(r.bucket) >= 3 && len(r.bucket) <= 63
}
func (r deliveryShardRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return artifactjson.Reservation{}, err
	}
	if !r.valid(scope) || sha != renderDerivativeBytesSHA(r.body) || size != int64(len(r.body)) {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	query := `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_shard($1,$2,$3,$4,$5,$6,$7,$8)`
	args := []any{r.lease.Expansion, r.lease.Token, r.purpose, string(r.body), r.bucket, scope.Tenant, scope.Run, scope.Owner}
	if r.purpose == "v30_delivery_assembly" {
		query = `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_assembly($1,$2,$3,$4::jsonb,$5,$6,$7,$8)`
		args[2], args[3] = string(r.body), string(r.assemblyProof)
	}
	err := r.db.QueryRow(ctx, query, args...).Scan(&outcome, &id, &token, &ready)
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
func (r deliveryShardRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
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
	query := `SELECT photo.complete_v30_delivery_shard($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	args := []any{r.lease.Expansion, r.lease.Token, r.purpose, string(r.body), r.bucket, ticket.ArtifactID, ticket.Token, object.VersionID, scope.Tenant, scope.Run, scope.Owner}
	if r.purpose == "v30_delivery_assembly" {
		query = `SELECT photo.complete_v30_delivery_assembly($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10,$11)`
		args[2], args[3] = string(r.body), string(r.assemblyProof)
	}
	err = r.db.QueryRow(ctx, query, args...).Scan(&accepted)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !accepted {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}

// NewDeliveryAssemblyPublisher owns the exact canonical shard bodies. SQL checks
// their current slot authority and immutable ready document identities on both
// sides of object IO. This does not retain closure or admit the assemble task.
func NewDeliveryAssemblyPublisher(db artifactjson.RowQuerier, objects artifactjson.Objects, bucket string, lease delivery.ExpansionLease, plan delivery.PreparePlan, shards [][]byte) (delivery.PublishDocument, error) {
	if _, err := NewDeliveryShardPublisher(db, objects, bucket, lease, plan); err != nil {
		return nil, err
	}
	if len(shards) != (lease.SlotCount+499)/500 {
		return nil, v2input.ErrInvalid
	}
	exact := make([]string, len(shards))
	for i, body := range shards {
		if len(body) < 2 || len(body) > 1<<20 {
			return nil, v2input.ErrInvalid
		}
		exact[i] = string(body)
	}
	proof, err := json.Marshal(exact)
	if err != nil {
		return nil, err
	}
	owner, err := v2input.TaskID(lease.Workflow, "v2.delivery.assemble", 0)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, purpose string, body []byte) (v2input.Ref, error) {
		if purpose != "v30_delivery_assembly" {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		repository := deliveryShardRepository{db: db, bucket: bucket, lease: lease, purpose: purpose, owner: owner, body: append([]byte(nil), body...), assemblyProof: proof}
		if !repository.valid(repository.scope()) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		ref, err := (artifactjson.Publisher{Repository: repository, Objects: objects}).Publish(ctx, repository.scope(), repository.body)
		if err != nil {
			return v2input.Ref{}, err
		}
		return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, nil
	}, nil
}
