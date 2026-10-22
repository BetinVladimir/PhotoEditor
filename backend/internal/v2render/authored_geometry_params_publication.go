package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type SQLAuthoredGeometryParamsPublication struct {
	Scheduler *pgxpool.Pool
	Objects   artifactjson.Objects
	Bucket    string
}
type authoredGeometryParamsDocuments struct {
	scheduler         *pgxpool.Pool
	scope             artifactjson.Scope
	ordinal           int
	bucket, body, sha string
	witnesses         []byte
}

func (r authoredGeometryParamsDocuments) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	zero := artifactjson.Reservation{}
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if r.scheduler == nil || scope != r.scope || sha != r.sha || size != int64(len(r.body)) {
		return zero, objectstore.ErrInvalid
	}
	var id, token uuid.UUID
	var ready bool
	e := database.TenantTransaction(ctx, r.scheduler, scope.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT * FROM photo.reserve_authored_render_geometry_params($1,$2,$3,$4,$5::jsonb,$6)`, scope.Tenant, scope.Run, r.ordinal, r.body, r.witnesses, r.bucket).Scan(&id, &token, &ready)
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
func (r authoredGeometryParamsDocuments) Complete(ctx context.Context, scope artifactjson.Scope, res artifactjson.Reservation, obj objectstore.Object) (artifactjson.Ref, error) {
	zero := artifactjson.Ref{}
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	target, e := artifactjson.DocumentKey(scope, res.ArtifactID)
	if e != nil || scope != r.scope || res.Token == uuid.Nil || obj.Key != target || obj.SHA256 != r.sha || obj.Bytes != int64(len(r.body)) || obj.ContentType != "application/json" || obj.VersionID == "" || obj.VersionID == "null" {
		return zero, objectstore.ErrIntegrity
	}
	var ok bool
	e = database.TenantTransaction(ctx, r.scheduler, scope.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.complete_authored_render_geometry_params($1,$2,$3,$4,$5,$6,$7,$8,$9)`, scope.Tenant, scope.Run, r.ordinal, res.ArtifactID, res.Token, obj.SHA256, obj.Bytes, r.bucket, obj.VersionID).Scan(&ok)
	})
	if e != nil {
		return zero, e
	}
	if !ok {
		return zero, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: res.ArtifactID, SHA256: r.sha}, nil
}
func (p SQLAuthoredGeometryParamsPublication) Publish(ctx context.Context, source AuthoredInstructionSource, compiled CompiledAuthoredGeometryTaskParams, witnesses AuthoredGeometryScopeWitnesses) (v2input.Ref, error) {
	zero := v2input.Ref{}
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if p.Scheduler == nil || p.Objects == nil || p.Bucket == "" || source.Instruction.Kind != "geometry" {
		return zero, v2input.ErrInvalid
	}
	checked, e := CompileAuthoredGeometryTaskParams(compiled.Params)
	if e != nil || !bytes.Equal(checked.JSON, compiled.JSON) {
		return zero, v2input.ErrInvalid
	}
	x := compiled.Params
	if x.TenantID != source.Tenant || x.ParentRunID != source.Parent || x.InstructionID != source.Instruction.Identity || x.ProgramSHA256 != source.ProgramSHA || x.PredecessorSHA256 != source.PredecessorSHA || x.Source != source.Source {
		return zero, v2input.ErrInvalid
	}
	before, found, e := ReadAuthoredGeometryPrefix(ctx, p.Scheduler, source.Tenant, source.Parent, source.Instruction.Ordinal)
	if e != nil {
		return zero, e
	}
	if !found {
		return zero, v2input.ErrExpired
	}
	a, e := canonical.Marshal(before.Context)
	if e != nil {
		return zero, e
	}
	b, e := canonical.Marshal(source)
	if e != nil || !bytes.Equal(a, b) {
		return zero, v2input.ErrExpired
	}
	raw, e := canonical.Marshal(witnesses)
	if e != nil {
		return zero, e
	}
	h := sha256.Sum256(compiled.JSON)
	scope := artifactjson.Scope{Tenant: source.Tenant, Run: source.Parent, Owner: source.Instruction.Identity, Purpose: "task_params"}
	repository := authoredGeometryParamsDocuments{scheduler: p.Scheduler, scope: scope, ordinal: source.Instruction.Ordinal, bucket: p.Bucket, body: string(compiled.JSON), sha: hex.EncodeToString(h[:]), witnesses: raw}
	ref, e := (artifactjson.Publisher{Repository: repository, Objects: p.Objects}).Publish(ctx, scope, compiled.JSON)
	if e != nil {
		return zero, e
	}
	after, present, e := ReadAuthoredGeometryPrefix(ctx, p.Scheduler, source.Tenant, source.Parent, source.Instruction.Ordinal)
	if e != nil {
		return zero, e
	}
	if !present {
		return zero, v2input.ErrExpired
	}
	a, e = canonical.Marshal(before)
	if e != nil {
		return zero, e
	}
	b, e = canonical.Marshal(after)
	if e != nil || !bytes.Equal(a, b) {
		return zero, v2input.ErrExpired
	}
	return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, nil
}

var _ artifactjson.Repository = authoredGeometryParamsDocuments{}
