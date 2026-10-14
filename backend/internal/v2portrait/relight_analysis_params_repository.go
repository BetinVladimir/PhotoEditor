package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type relightAnalysisParamsRepository struct {
	db     DonorConfirmationTransactions
	bucket string
	tenant uuid.UUID
	who    auth.Identity
	plan   RelightAnalysisPlan
}

func (r relightAnalysisParamsRepository) proof(scope artifactjson.Scope, sha string, size int64) (string, error) {
	p := r.plan.Snapshot()
	if r.db == nil || r.bucket == "" || scope.Tenant != r.tenant || scope.Run != p.Run || scope.Owner != p.Task || scope.Purpose != "task_params" || scope.AssemblyLease != uuid.Nil || scope.LocalPortrait || scope.DonorPortrait || sha != p.ParamsSHA || size != int64(len(p.Params)) {
		return "", v2input.ErrInvalid
	}
	var identity struct {
		Tenant  uuid.UUID `json:"tenant_id"`
		Actor   uuid.UUID `json:"actor_id"`
		Session uuid.UUID `json:"auth_session_id"`
	}
	if json.Unmarshal(p.Config, &identity) != nil || identity.Tenant != r.tenant || identity.Actor != r.who.UserID || identity.Session != r.who.SessionID || r.who.UserID == uuid.Nil || r.who.SessionID == uuid.Nil {
		return "", v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(map[string]any{"plan": p, "params_jcs": string(p.Params)})
	return string(raw), err
}
func (r relightAnalysisParamsRepository) authorize(ctx context.Context, tx pgx.Tx, scope artifactjson.Scope) error {
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	_, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", scope.Tenant.String())
	return err
}
func (r relightAnalysisParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var zero artifactjson.Reservation
	proof, err := r.proof(scope, sha, size)
	if err != nil {
		return zero, err
	}
	if r.db == nil {
		return zero, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.Background())
	if err = r.authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v28_relight_analysis_params_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.bucket, proof).Scan(&outcome, &id, &token, &ready)
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
	if *id != r.plan.snapshot.ParamsDocument {
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

func (r relightAnalysisParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	proof, err := r.proof(scope, object.SHA256, object.Bytes)
	if err != nil {
		return zero, err
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key || ticket.Token == uuid.Nil || ticket.ArtifactID != r.plan.snapshot.ParamsDocument || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || r.db == nil {
		return zero, objectstore.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.Background())
	if err = r.authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var done bool
	err = tx.QueryRow(ctx, "SELECT photo.complete_v28_relight_analysis_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.bucket, object.VersionID, proof).Scan(&done)
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

// PublishRelightAnalysisParams commits the saved draft and reservation before
// immutable object IO, then repeats current stored-plan authority at completion.
func PublishRelightAnalysisParams(ctx context.Context, db DonorConfirmationTransactions, objects artifactjson.Objects, bucket string, who auth.Identity, tenant uuid.UUID, plan RelightAnalysisPlan) (artifactjson.Ref, error) {
	if objects == nil || bucket == "" {
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	if err := SaveRelightAnalysisDraft(ctx, db, who, tenant, plan); err != nil {
		return artifactjson.Ref{}, err
	}
	p := plan.Snapshot()
	scope := artifactjson.Scope{Tenant: tenant, Run: p.Run, Owner: p.Task, Purpose: "task_params"}
	return (artifactjson.Publisher{Repository: relightAnalysisParamsRepository{db: db, bucket: bucket, tenant: tenant, who: who, plan: plan}, Objects: objects}).Publish(ctx, scope, p.Params)
}
