package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// AuthoredSkinParamsPublication uses the actual immutable/version-verified JSON
// publisher. The protected draft owns the exact bytes before this writer starts;
// both SQL boundaries recheck its live source and qualification independently.
type AuthoredSkinParamsPublication struct {
	Scheduler *pgxpool.Pool
	Objects   artifactjson.Objects
	Bucket    string
}

func (p AuthoredSkinParamsPublication) Publish(ctx context.Context, source AuthoredInstructionSource, operation string, params v2input.Ref, raw []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	raw = append([]byte(nil), raw...)
	run, workflow, err := AuthoredSkinPrerequisiteIDs(source, operation)
	if err != nil || p.Scheduler == nil || p.Objects == nil || p.Bucket == "" || !validLayerArtifactRef(params) || len(raw) < 2 || len(raw) > 1048576 || !json.Valid(raw) || authoredFaceCandidateSHA(raw) != params.SHA256 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, operation, 0)
	if err != nil {
		return v2input.Ref{}, err
	}
	scope := artifactjson.Scope{Tenant: source.Tenant, Run: run, Owner: task, Purpose: "task_params"}
	repo := authoredSkinParamsRepository{db: p.Scheduler, scope: scope, parent: source.Parent, ordinal: source.Instruction.Ordinal, operation: operation, bucket: p.Bucket, expected: params, size: int64(len(raw))}
	out, err := (artifactjson.Publisher{Repository: repo, Objects: p.Objects}).Publish(ctx, scope, append([]byte(nil), raw...))
	if err != nil {
		return v2input.Ref{}, err
	}
	if out.ID != params.ArtifactID || out.SHA256 != params.SHA256 {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	return params, ctx.Err()
}

type authoredSkinParamsRepository struct {
	db                *pgxpool.Pool
	scope             artifactjson.Scope
	parent            uuid.UUID
	ordinal           int
	operation, bucket string
	expected          v2input.Ref
	size              int64
}

func (r authoredSkinParamsRepository) transaction(ctx context.Context, fn func(pgx.Tx) error) error {
	return database.TenantTransaction(ctx, r.db, r.scope.Tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return fn(tx)
	})
}
func (r authoredSkinParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var out artifactjson.Reservation
	var ready bool
	if r.db == nil || scope != r.scope || sha != r.expected.SHA256 || size != r.size {
		return out, objectstore.ErrInvalid
	}
	err := r.transaction(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT * FROM photo.reserve_authored_render_skin_params($1,$2,$3,$4,$5)", scope.Tenant, r.parent, r.ordinal, r.operation, r.bucket).Scan(&out.ArtifactID, &out.Token, &ready)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, artifactjson.ErrStale
	}
	if err != nil {
		return out, err
	}
	if out.ArtifactID != r.expected.ArtifactID || out.Token == uuid.Nil {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	if ready {
		out.Ready = &artifactjson.Ref{ID: out.ArtifactID, SHA256: sha}
	}
	return out, nil
}
func (r authoredSkinParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, res artifactjson.Reservation, obj objectstore.Object) (artifactjson.Ref, error) {
	key, err := artifactjson.DocumentKey(scope, res.ArtifactID)
	if err != nil || scope != r.scope || res.ArtifactID != r.expected.ArtifactID || res.Token == uuid.Nil || obj.Key != key || obj.SHA256 != r.expected.SHA256 || obj.Bytes != r.size || obj.ContentType != "application/json" || obj.VersionID == "" || obj.VersionID == "null" {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var accepted bool
	err = r.transaction(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.complete_authored_render_skin_params($1,$2,$3,$4,$5,$6,$7,$8)", scope.Tenant, r.parent, r.ordinal, r.operation, res.ArtifactID, res.Token, r.bucket, obj.VersionID).Scan(&accepted)
	})
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !accepted {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: res.ArtifactID, SHA256: obj.SHA256}, ctx.Err()
}

var _ artifactjson.Repository = authoredSkinParamsRepository{}
