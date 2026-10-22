package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type SQLAuthoredNativeParamsPublication struct {
	Scheduler *pgxpool.Pool
	Objects   artifactjson.Objects
	Bucket    string
}
type authoredNativeParamsRepository struct {
	scheduler *pgxpool.Pool
	scope     artifactjson.Scope
	m         AuthoredNativeMaterialization
	bucket    string
}

func (r authoredNativeParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var zero artifactjson.Reservation
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if r.scheduler == nil || scope != r.scope || sha != r.m.selected.Params.SHA256 || size != int64(len(r.m.selected.Params.Body)) {
		return zero, v2input.ErrInvalid
	}
	var id, token uuid.UUID
	var ready bool
	e := database.TenantTransaction(ctx, r.scheduler, scope.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT * FROM photo.reserve_authored_native_params($1,$2,$3,$4,$5,$6,$7)`, scope.Tenant, scope.Run, r.m.source.Instruction.Ordinal, r.m.selected.Operation, string(r.m.authored), string(r.m.selected.Params.Body), r.bucket).Scan(&id, &token, &ready)
	})
	if errors.Is(e, pgx.ErrNoRows) {
		return zero, artifactjson.ErrStale
	}
	if e != nil {
		return zero, e
	}
	if id == uuid.Nil || token == uuid.Nil {
		return zero, objectstore.ErrIntegrity
	}
	out := artifactjson.Reservation{ArtifactID: id, Token: token}
	if ready {
		out.Ready = &artifactjson.Ref{ID: id, SHA256: sha}
	}
	return out, nil
}
func (r authoredNativeParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, res artifactjson.Reservation, obj objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	key, e := artifactjson.DocumentKey(scope, res.ArtifactID)
	if e != nil || scope != r.scope || res.Token == uuid.Nil || obj.Key != key || obj.SHA256 != r.m.selected.Params.SHA256 || obj.Bytes != int64(len(r.m.selected.Params.Body)) || obj.ContentType != "application/json" || obj.VersionID == "" || obj.VersionID == "null" {
		return zero, objectstore.ErrIntegrity
	}
	var ok bool
	e = database.TenantTransaction(ctx, r.scheduler, scope.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.complete_authored_native_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, scope.Tenant, scope.Run, r.m.source.Instruction.Ordinal, r.m.selected.Operation, string(r.m.authored), string(r.m.selected.Params.Body), res.ArtifactID, res.Token, r.bucket, obj.VersionID).Scan(&ok)
	})
	if e != nil {
		return zero, e
	}
	if !ok {
		return zero, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: res.ArtifactID, SHA256: r.m.selected.Params.SHA256}, nil
}
func (s SQLAuthoredNativeParamsPublication) Publish(ctx context.Context, m AuthoredNativeMaterialization) (v2input.Ref, error) {
	var zero v2input.Ref
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if s.Scheduler == nil || s.Objects == nil || s.Bucket == "" || len(m.proof) < 2 {
		return zero, v2input.ErrInvalid
	}
	_, _, task, e := authoredNativeIdentities(m)
	if e != nil {
		return zero, e
	}
	scope := artifactjson.Scope{Tenant: m.source.Tenant, Run: m.source.Parent, Owner: task, Purpose: "task_params"}
	repository := authoredNativeParamsRepository{scheduler: s.Scheduler, scope: scope, m: m, bucket: s.Bucket}
	ref, e := (artifactjson.Publisher{Repository: repository, Objects: s.Objects}).Publish(ctx, scope, append([]byte(nil), m.selected.Params.Body...))
	if e != nil {
		return zero, e
	}
	if ref.SHA256 != m.selected.Params.SHA256 {
		return zero, objectstore.ErrIntegrity
	}
	return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, ctx.Err()
}

var _ artifactjson.Repository = authoredNativeParamsRepository{}
