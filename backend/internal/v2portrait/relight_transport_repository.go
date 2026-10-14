package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type relightTransportRepository struct {
	coordinator RelightBootstrapCoordinator
	prepared    RelightBootstrapResult
	body        string
}

func (r relightTransportRepository) proof(scope artifactjson.Scope, sha string, size int64) (string, error) {
	w := r.prepared.prepared
	if w == nil || scope.Tenant != w.request.Tenant || scope.Run != w.request.Run || scope.Owner != w.intent.OwnerID || scope.AssemblyLease != uuid.Nil || scope.LocalPortrait || scope.DonorPortrait || size != int64(len(r.body)) || size < 2 || size > 1048576 || (scope.Purpose != "v2_input_chunk_0" && scope.Purpose != "v2_input_chunk_1" && scope.Purpose != "v2_input_manifest") {
		return "", v2input.ErrInvalid
	}
	sum := sha256.Sum256([]byte(r.body))
	if sha != hex.EncodeToString(sum[:]) {
		return "", v2input.ErrInvalid
	}
	paramsScope := artifactjson.Scope{Tenant: scope.Tenant, Run: scope.Run, Owner: scope.Owner, Purpose: "task_params"}
	return (relightParamsRepository{coordinator: r.coordinator, prepared: r.prepared}).proof(paramsScope, w.intent.ParamsSHA, int64(len(w.intent.ParamsJCS)))
}

func (r relightTransportRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var zero artifactjson.Reservation
	proof, err := r.proof(scope, sha, size)
	if err != nil {
		return zero, err
	}
	if r.coordinator.DB == nil {
		return zero, v2input.ErrInvalid
	}
	tx, err := r.coordinator.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	if err = r.coordinator.RevalidatePrepared(ctx, tx, r.prepared); err != nil {
		return zero, err
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v28_relight_transport_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.coordinator.Bucket, proof, r.body).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return zero, err
	}
	switch outcome {
	case "quota":
		return zero, artifactjson.ErrQuota
	case "conflict":
		return zero, artifactjson.ErrConflict
	case "forbidden", "stale":
		return zero, artifactjson.ErrStale
	case "created", "resumed", "replayed":
	default:
		return zero, objectstore.ErrIntegrity
	}
	if id == nil || *id == uuid.Nil || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return zero, objectstore.ErrIntegrity
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

func (r relightTransportRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	proof, err := r.proof(scope, object.SHA256, object.Bytes)
	if err != nil {
		return zero, err
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key || ticket.Token == uuid.Nil || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || r.coordinator.DB == nil {
		return zero, objectstore.ErrInvalid
	}
	tx, err := r.coordinator.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	if err = r.coordinator.RevalidatePrepared(ctx, tx, r.prepared); err != nil {
		return zero, err
	}
	var done bool
	err = tx.QueryRow(ctx, "SELECT photo.complete_v28_relight_transport_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.coordinator.Bucket, object.VersionID, proof, r.body).Scan(&done)
	if err != nil {
		return zero, err
	}
	if !done {
		return zero, artifactjson.ErrStale
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}

type relightTransportDocuments struct {
	coordinator RelightBootstrapCoordinator
	prepared    RelightBootstrapResult
	objects     artifactjson.Objects
}

func (p relightTransportDocuments) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	repository := relightTransportRepository{coordinator: p.coordinator, prepared: p.prepared, body: string(body)}
	return (artifactjson.Publisher{Repository: repository, Objects: p.objects}).Publish(ctx, scope, body)
}

// PublishRelightTransport stages the exact blocked task, commits, then publishes
// each exact bounded chunk and manifest with dedicated01005 authority. Each PUT
// is outside transactions and completion repeats all current authority. Returned
// transport still requires transactional input registration and admission.
func (s RelightBootstrapCoordinator) PublishRelightTransport(ctx context.Context, objects artifactjson.Objects, prepared RelightBootstrapResult, frozen FrozenRelightTask) (v2input.GraphTask, error) {
	if objects == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	if err := s.StageRelightTask(ctx, prepared, frozen); err != nil {
		return v2input.GraphTask{}, err
	}
	w := prepared.prepared
	documents := relightTransportDocuments{coordinator: s, prepared: prepared, objects: objects}
	return frozen.PublishInputClosure(ctx, v2input.ScopedPublisher{Documents: documents, Tenant: w.request.Tenant, Run: w.request.Run})
}
