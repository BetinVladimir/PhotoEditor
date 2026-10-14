package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type relightAnalysisTransportRepository struct {
	coordinator RelightAnalysisCoordinator
	prepared    PreparedRelightAnalysis
	body        string
}

func (r relightAnalysisTransportRepository) proof(scope artifactjson.Scope, sha string, size int64) (string, error) {
	s := r.prepared.plan.Snapshot()
	var identity struct {
		Tenant  uuid.UUID `json:"tenant_id"`
		Actor   uuid.UUID `json:"actor_id"`
		Session uuid.UUID `json:"auth_session_id"`
	}
	if json.Unmarshal(s.Config, &identity) != nil || scope.Tenant != identity.Tenant || scope.Run != s.Run || scope.Owner != s.Task || scope.AssemblyLease != uuid.Nil || scope.LocalPortrait || scope.DonorPortrait || size != int64(len(r.body)) || size < 2 || size > 1048576 || (scope.Purpose != "v2_input_chunk_0" && scope.Purpose != "v2_input_manifest") {
		return "", v2input.ErrInvalid
	}
	sum := sha256.Sum256([]byte(r.body))
	if sha != hex.EncodeToString(sum[:]) {
		return "", v2input.ErrInvalid
	}
	paramsScope := artifactjson.Scope{Tenant: scope.Tenant, Run: scope.Run, Owner: scope.Owner, Purpose: "task_params"}
	return (relightAnalysisParamsRepository{db: r.coordinator.DB, bucket: r.coordinator.Bucket, tenant: scope.Tenant, who: auth.Identity{UserID: identity.Actor, SessionID: identity.Session}, plan: r.prepared.plan}).proof(paramsScope, s.ParamsSHA, int64(len(s.Params)))
}

func (r relightAnalysisTransportRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var zero artifactjson.Reservation
	proof, err := r.proof(scope, sha, size)
	if err != nil {
		return zero, err
	}
	if r.coordinator.DB == nil {
		return zero, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.coordinator.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = (relightAnalysisParamsRepository{}).authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v28_relight_analysis_transport_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.coordinator.Bucket, proof, r.body).Scan(&outcome, &id, &token, &ready)
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

func (r relightAnalysisTransportRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	proof, err := r.proof(scope, object.SHA256, object.Bytes)
	if err != nil {
		return zero, err
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key || ticket.Token == uuid.Nil || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || r.coordinator.DB == nil {
		return zero, objectstore.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.coordinator.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = (relightAnalysisParamsRepository{}).authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var done bool
	err = tx.QueryRow(ctx, "SELECT photo.complete_v28_relight_analysis_transport_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.coordinator.Bucket, object.VersionID, proof, r.body).Scan(&done)
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

type relightAnalysisTransportDocuments struct {
	coordinator RelightAnalysisCoordinator
	prepared    PreparedRelightAnalysis
	objects     artifactjson.Objects
}

func (p relightAnalysisTransportDocuments) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	repository := relightAnalysisTransportRepository{coordinator: p.coordinator, prepared: p.prepared, body: string(body)}
	return (artifactjson.Publisher{Repository: repository, Objects: p.objects}).Publish(ctx, scope, body)
}

// PublishTransport stages and commits the blocked task before external IO.
// Dedicated SQL publication revalidates saved plan and exact chunk/manifest.
func (c RelightAnalysisCoordinator) PublishTransport(ctx context.Context, who auth.Identity, request RelightAnalysisPlanRequest, prepared PreparedRelightAnalysis) (v2input.GraphTask, error) {
	if c.Writer == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	if err := c.Stage(ctx, who, request, prepared); err != nil {
		return v2input.GraphTask{}, err
	}
	documents := relightAnalysisTransportDocuments{coordinator: c, prepared: prepared, objects: c.Writer}
	return prepared.plan.PublishInputClosure(ctx, v2input.ScopedPublisher{Documents: documents, Tenant: request.Tenant, Run: prepared.plan.snapshot.Run})
}
