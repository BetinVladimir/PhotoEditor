package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
)

type EditorEffectsObjects interface {
	ArtifactOpener
	artifactjson.Objects
}

// One bounded origin per scheduler pass; every write repeats current source,
// actor and producer authority. Original Apply completion is only a prestage.
type EditorEffectsCoordinator struct {
	DB           *pgxpool.Pool
	Objects      EditorEffectsObjects
	Bucket       string
	Calibrations v2render.RenderCalibrationCatalog
	after        uuid.UUID
	afterFinal   uuid.UUID
}

func (c *EditorEffectsCoordinator) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c == nil || c.DB == nil || c.Objects == nil || c.Bucket == "" {
		return false, objectstore.ErrInvalid
	}
	var settled bool
	if err := c.DB.QueryRow(ctx, `SELECT photo.billing_settle_next_editor()`).Scan(&settled); err != nil {
		return false, err
	}
	if settled {
		return true, nil
	}
	if err := c.DB.QueryRow(ctx, `SELECT photo.withdraw_editor_effect_build()`).Scan(&settled); err != nil {
		return false, err
	}
	if settled {
		return true, nil
	}
	if err := c.DB.QueryRow(ctx, `SELECT photo.settle_editor_effect_final_render()`).Scan(&settled); err != nil {
		return false, err
	}
	if settled {
		return true, nil
	}
	var tid, build uuid.UUID
	var origin, trace, inventory []byte
	var cursor *uuid.UUID
	if c.after != uuid.Nil {
		cursor = &c.after
	}
	err := c.DB.QueryRow(ctx, `SELECT * FROM photo.editor_effect_seal_context($1)`, cursor).Scan(&tid, &build, &origin, &trace, &inventory)
	if errors.Is(err, pgx.ErrNoRows) {
		c.after = uuid.Nil
		return c.processFinalOne(ctx)
	}
	if err != nil {
		return false, err
	}
	c.after = build
	document, err := buildEditorSealDocument(ctx, c.Objects, c.Bucket, origin, trace, inventory)
	if err != nil {
		return false, err
	}
	run := uuid.NewSHA1(build, []byte("editor.effects_seal/run"))
	task := uuid.NewSHA1(run, []byte("editor.effects_seal"))
	inputs, err := canonical.Marshal(document.Inputs)
	if err != nil {
		return false, err
	}
	var accepted *uuid.UUID
	err = c.DB.QueryRow(ctx, `SELECT photo.prepare_editor_effect_seal($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb)`, tid, build, run, task, string(document.Params), string(inputs), string(document.Profile)).Scan(&accepted)
	if err != nil {
		return false, err
	}
	if accepted == nil {
		return false, artifactjson.ErrStale
	}
	if *accepted != run {
		return false, objectstore.ErrIntegrity
	}
	scope := artifactjson.Scope{Tenant: tid, Run: run, Owner: task, Purpose: "task_params"}
	repository := editorSealDocuments{db: c.DB, scope: scope, build: build, bucket: c.Bucket, sha: document.SHA, bytes: int64(len(document.Params))}
	params, err := (artifactjson.Publisher{Repository: repository, Objects: c.Objects}).Publish(ctx, scope, document.Params)
	if err != nil {
		return false, err
	}
	paramRef := review.ArtifactRef{ID: params.ID, SHA256: params.SHA256}
	digest, err := repairInputDigest("editor.effects_seal", "1", paramRef, document.Inputs)
	if err != nil {
		return false, err
	}
	prepared := PreparedTask{ID: task, NodeKey: "editor.effects_seal", Operation: "editor.effects_seal", Version: "1", ResourceClass: "cpu", TimeoutSeconds: 300, Params: paramRef, Inputs: document.Inputs, InputDigest: digest, ExecutionProfile: document.Profile}
	raw, err := canonical.Marshal(prepared)
	if err != nil {
		return false, err
	}
	var sealed bool
	err = c.DB.QueryRow(ctx, `SELECT photo.materialize_editor_effect_seal($1,$2,$3::jsonb)`, tid, build, string(raw)).Scan(&sealed)
	if err != nil {
		return false, err
	}
	if !sealed {
		return false, artifactjson.ErrStale
	}
	return true, nil
}

type editorSealDocuments struct {
	db          *pgxpool.Pool
	scope       artifactjson.Scope
	build       uuid.UUID
	bucket, sha string
	bytes       int64
}

func (r editorSealDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if r.db == nil || s != r.scope || sha != r.sha || size != r.bytes {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var id, token uuid.UUID
	var ready bool
	err := r.db.QueryRow(ctx, `SELECT * FROM photo.reserve_editor_effect_seal_document($1,$2,$3)`, s.Tenant, r.build, r.bucket).Scan(&id, &token, &ready)
	if errors.Is(err, pgx.ErrNoRows) {
		return artifactjson.Reservation{}, artifactjson.ErrStale
	}
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	if id == uuid.Nil || token == uuid.Nil {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	value := artifactjson.Reservation{ArtifactID: id, Token: token}
	if ready {
		value.Ready = &artifactjson.Ref{ID: id, SHA256: sha}
	}
	return value, nil
}
func (r editorSealDocuments) Complete(ctx context.Context, s artifactjson.Scope, res artifactjson.Reservation, obj objectstore.Object) (artifactjson.Ref, error) {
	key, err := artifactjson.DocumentKey(s, res.ArtifactID)
	if err != nil || s != r.scope || obj.Key != key || obj.SHA256 != r.sha || obj.Bytes != r.bytes || obj.ContentType != "application/json" || obj.VersionID == "" || obj.VersionID == "null" {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var complete bool
	err = r.db.QueryRow(ctx, `SELECT photo.complete_editor_effect_seal_document($1,$2,$3,$4,$5,$6,$7,$8)`, s.Tenant, r.build, res.ArtifactID, res.Token, obj.SHA256, obj.Bytes, r.bucket, obj.VersionID).Scan(&complete)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !complete {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: res.ArtifactID, SHA256: r.sha}, nil
}

var _ artifactjson.Repository = editorSealDocuments{}
