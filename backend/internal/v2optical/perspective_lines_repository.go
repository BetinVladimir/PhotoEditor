package v2optical

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type perspectiveLinesRepository struct {
	db     PerspectiveTransactions
	bucket string
	tenant uuid.UUID
	who    auth.Identity
	plan   PerspectiveLinesDraft
}

func (r perspectiveLinesRepository) proof(scope artifactjson.Scope, sha string, size int64) (string, error) {
	p, err := r.plan.Snapshot()
	if err != nil {
		return "", err
	}
	if r.db == nil || r.bucket == "" || scope.Tenant != r.tenant || scope.Run != p.Run || scope.Owner != p.Owner || scope.Purpose != "task_params" || scope.AssemblyLease != uuid.Nil || scope.LocalPortrait || scope.DonorPortrait || sha != p.SHA256 || size != int64(len(p.LinesJCS)) {
		return "", v2input.ErrInvalid
	}
	if p.Tenant != r.tenant || p.Actor != r.who.UserID || p.Session != r.who.SessionID || r.who.UserID == uuid.Nil || r.who.SessionID == uuid.Nil {
		return "", v2input.ErrInvalid
	}

	raw, err := canonical.Marshal(map[string]any{"plan": p})
	return string(raw), err
}
func (r perspectiveLinesRepository) authorize(ctx context.Context, tx pgx.Tx, scope artifactjson.Scope) error {
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
func (r perspectiveLinesRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
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
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_perspective_lines_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, sha, size, r.bucket, proof).Scan(&outcome, &id, &token, &ready)
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
	planSnapshot, err := r.plan.Snapshot()
	if err != nil {
		return zero, err
	}
	if *id != planSnapshot.Artifact {
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

func (r perspectiveLinesRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	proof, err := r.proof(scope, object.SHA256, object.Bytes)
	if err != nil {
		return zero, err
	}
	planSnapshot, err := r.plan.Snapshot()
	if err != nil {
		return zero, err
	}
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || object.Key != key || ticket.Token == uuid.Nil || ticket.ArtifactID != planSnapshot.Artifact || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || r.db == nil {
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
	err = tx.QueryRow(ctx, "SELECT photo.complete_v29_perspective_lines_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.bucket, object.VersionID, proof).Scan(&done)
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

// PublishPerspectiveLines commits the saved draft and reservation before
// immutable object IO, then repeats current stored-plan authority at completion.
func PublishPerspectiveLines(ctx context.Context, db PerspectiveTransactions, objects artifactjson.Objects, bucket string, who auth.Identity, tenant uuid.UUID, plan PerspectiveLinesDraft) (artifactjson.Ref, error) {
	if objects == nil || bucket == "" {
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	if err := SavePerspectiveLinesDraft(ctx, db, who, tenant, plan); err != nil {
		return artifactjson.Ref{}, err
	}
	p, err := plan.Snapshot()
	if err != nil {
		return artifactjson.Ref{}, err
	}
	scope := artifactjson.Scope{Tenant: tenant, Run: p.Run, Owner: p.Owner, Purpose: "task_params"}
	return (artifactjson.Publisher{Repository: perspectiveLinesRepository{db: db, bucket: bucket, tenant: tenant, who: who, plan: plan}, Objects: objects}).Publish(ctx, scope, []byte(p.LinesJCS))
}
