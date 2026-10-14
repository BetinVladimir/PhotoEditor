package v2portrait

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type relightParamsRepository struct {
	coordinator RelightBootstrapCoordinator
	prepared    RelightBootstrapResult
}

func (r relightParamsRepository) proof(scope artifactjson.Scope, sha string, size int64) (string, error) {
	w := r.prepared.prepared
	if w == nil || r.prepared.Intent != w.intent || scope.Tenant != w.request.Tenant || scope.Run != w.request.Run || scope.Owner != w.intent.OwnerID || scope.Purpose != "task_params" || scope.AssemblyLease != uuid.Nil || scope.LocalPortrait || scope.DonorPortrait || sha != w.intent.ParamsSHA || size != int64(len(w.intent.ParamsJCS)) {
		return "", v2input.ErrInvalid
	}
	type manualProof struct {
		Protected bool   `json:"protected"`
		Root      string `json:"root"`
	}
	proof := struct {
		Depth     string        `json:"depth"`
		Subject   string        `json:"subject"`
		Protected string        `json:"protected"`
		Manual    []manualProof `json:"manual"`
	}{Depth: w.depthBody, Subject: w.subjectBody, Protected: w.protectedBody}
	for _, m := range w.manuals {
		if m.preintent == nil {
			return "", v2input.ErrInvalid
		}
		proof.Manual = append(proof.Manual, manualProof{Protected: m.preintent.selection == "manual_guard", Root: string(m.RootBody)})
	}
	raw, err := canonical.Marshal(proof)
	return string(raw), err
}

func (r relightParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
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
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v28_relight_params_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.coordinator.Bucket, proof).Scan(&outcome, &id, &token, &ready)
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

func (r relightParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
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
	err = tx.QueryRow(ctx, "SELECT photo.complete_v28_relight_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.coordinator.Bucket, object.VersionID, proof).Scan(&done)
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

// PublishPreparedRelightParams commits reservation before object IO and repeats
// full retained authority in completion. No task or transport is activated.
// Prepared witnesses are in-process only; restart recovery must prepare again.
func (s RelightBootstrapCoordinator) PublishPreparedRelightParams(ctx context.Context, objects artifactjson.Objects, prepared RelightBootstrapResult) (artifactjson.Ref, error) {
	w := prepared.prepared
	if w == nil || prepared.Intent != w.intent {
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	scope := artifactjson.Scope{Tenant: w.request.Tenant, Run: w.request.Run, Owner: w.intent.OwnerID, Purpose: "task_params"}
	return (artifactjson.Publisher{Repository: relightParamsRepository{coordinator: s, prepared: prepared}, Objects: objects}).Publish(ctx, scope, []byte(w.intent.ParamsJCS))
}
