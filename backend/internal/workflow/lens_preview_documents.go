package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
	"time"
)

// Pool-backed calls complete before Publisher object IO; frozen strings own authored evidence.
type LensPreviewDocuments struct {
	db                                        *pgxpool.Pool
	scope                                     artifactjson.Scope
	bucket, variant, sha, root, image, params string
}

var _ artifactjson.Repository = (*LensPreviewDocuments)(nil)

func NewLensPreviewDocuments(db *pgxpool.Pool, tenant uuid.UUID, bucket string, document RenderDerivativeDocument, root, image []byte) (*LensPreviewDocuments, error) {
	if db == nil || tenant == uuid.Nil || document.Run == uuid.Nil || document.ID != uuid.NewSHA1(document.Run, []byte("render.derivative:"+document.Variant)) || len(bucket) < 3 || len(bucket) > 63 || len(document.Params) < 2 || len(document.Params) > 1048576 || renderDerivativeBytesSHA(document.Params) != document.ParamsSHA || len(root) < 2 || len(root) > 1048576 || len(image) < 2 || len(image) > 1048576 || (document.Variant != "preview1600" && document.Variant != "preview2560") {
		return nil, objectstore.ErrInvalid
	}
	return &LensPreviewDocuments{db: db, scope: artifactjson.Scope{Tenant: tenant, Run: document.Run, Owner: document.ID, Purpose: "task_params"}, bucket: bucket, variant: document.Variant, sha: document.ParamsSHA, root: string(root), image: string(image), params: string(document.Params)}, nil
}
func (r *LensPreviewDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if r == nil || r.db == nil || s != r.scope || sha != r.sha || size != int64(len(r.params)) {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var id, token uuid.UUID
	var ready bool
	err := withLensPreviewDocumentTx(ctx, r.db, s.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT * FROM photo.reserve_v25_lens_preview_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", s.Tenant, s.Run, s.Owner, r.variant, sha, size, r.bucket, r.root, r.image, r.params).Scan(&id, &token, &ready)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return artifactjson.Reservation{}, artifactjson.ErrStale
	}
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	if id == uuid.Nil || token == uuid.Nil {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	reservation := artifactjson.Reservation{ArtifactID: id, Token: token}
	if ready {
		reservation.Ready = &artifactjson.Ref{ID: id, SHA256: sha}
	}
	return reservation, nil
}
func (r *LensPreviewDocuments) Complete(ctx context.Context, s artifactjson.Scope, v artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	if r == nil || r.db == nil || s != r.scope || v.ArtifactID == uuid.Nil || v.Token == uuid.Nil || o.SHA256 != r.sha || o.Bytes != int64(len(r.params)) || o.ContentType != "application/json" || o.VersionID == "" || o.VersionID == "null" {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	key, err := artifactjson.DocumentKey(s, v.ArtifactID)
	if err != nil || o.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var completed bool
	err = withLensPreviewDocumentTx(ctx, r.db, s.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.complete_v25_lens_preview_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", s.Tenant, s.Run, s.Owner, v.ArtifactID, v.Token, o.SHA256, o.Bytes, r.bucket, o.VersionID, r.root, r.image).Scan(&completed)
	})
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !completed {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: v.ArtifactID, SHA256: o.SHA256}, nil
}

func withLensPreviewDocumentTx(ctx context.Context, db *pgxpool.Pool, tenant uuid.UUID, fn func(context.Context, pgx.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil || tenant == uuid.Nil {
		return objectstore.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return objectstore.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	if err = fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PublishLensPreviewTasks requires saved intent and live reservation.
// SQL repeats original source authority at each reserve and completion.
func PublishLensPreviewTasks(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, plan LensPreviewPlan, bundle v2render.LensPreviewBundle) ([]PreparedTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateLensPreviewIntent(plan); err != nil {
		return nil, err
	}
	if db == nil || objects == nil || bundle.Source() != plan.source {
		return nil, objectstore.ErrInvalid
	}
	rebuilt, err := CompileLensPreviewPlan(bundle, plan.srgb,plan.policy)
	if err != nil {
		return nil, err
	}
	if rebuilt.run != plan.run {
		return nil, objectstore.ErrInvalid
	}
	originals := bundle.Documents()
	result := bundle.Result()
	root, image := originals[plan.source.Result.ArtifactID.String()], originals[result.WorkingImage.ArtifactID.String()]
	documents := rebuilt.Documents()
	repositories := make([]*LensPreviewDocuments, len(documents))
	for i, d := range documents {
		repositories[i], err = NewLensPreviewDocuments(db, plan.source.Tenant, bucket, d, root, image)
		if err != nil {
			return nil, err
		}
	}
	tasks := make([]PreparedTask, 0, len(documents))
	for i, d := range documents {
		repository := repositories[i]
		ref, err := (artifactjson.Publisher{Repository: repository, Objects: objects}).Publish(ctx, repository.scope, []byte(repository.params))
		if err != nil {
			return nil, err
		}
		task, err := PrepareRenderDerivativeTask(d, review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256})
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}
