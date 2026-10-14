package workflow

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// One repository owns exactly one frozen side/variant Params document.
type RestorationPreviewDocuments struct {
	db                                        *pgxpool.Pool
	scope                                     artifactjson.Scope
	bucket, side, variant, sha, image, params string
}

var _ artifactjson.Repository = (*RestorationPreviewDocuments)(nil)

func NewRestorationPreviewDocuments(db *pgxpool.Pool, tenant uuid.UUID, bucket string,
	doc RestorationPreviewDocument, image []byte) (*RestorationPreviewDocuments, error) {
	if db == nil || tenant == uuid.Nil || doc.Run == uuid.Nil ||
		(doc.Side != "before" && doc.Side != "after") ||
		(doc.Variant != "preview1600" && doc.Variant != "preview2560") ||
		doc.ID != restorationPreviewTaskID(doc.Run, doc.Side, doc.Variant) ||
		len(bucket) < 3 || len(bucket) > 63 || len(image) < 2 || len(image) > 1048576 ||
		len(doc.Params) < 2 || len(doc.Params) > 1048576 ||
		renderDerivativeBytesSHA(doc.Params) != doc.ParamsSHA {
		return nil, objectstore.ErrInvalid
	}
	return &RestorationPreviewDocuments{db: db,
		scope:  artifactjson.Scope{Tenant: tenant, Run: doc.Run, Owner: doc.ID, Purpose: "task_params"},
		bucket: bucket, side: doc.Side, variant: doc.Variant,
		sha: doc.ParamsSHA, image: string(image), params: string(doc.Params)}, nil
}

func (r *RestorationPreviewDocuments) Reserve(ctx context.Context, scope artifactjson.Scope,
	sha string, size int64) (artifactjson.Reservation, error) {
	if r == nil || r.db == nil || scope != r.scope || sha != r.sha ||
		size != int64(len(r.params)) {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var id, token uuid.UUID
	var ready bool
	err := withPerspectivePreviewDocumentTx(ctx, r.db, scope.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT * FROM photo.reserve_v28_restoration_preview_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)",
			scope.Tenant, scope.Run, scope.Owner, r.side, r.variant, sha, size,
			r.bucket, r.image, r.params).Scan(&id, &token, &ready)
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

func (r *RestorationPreviewDocuments) Complete(ctx context.Context, scope artifactjson.Scope,
	reservation artifactjson.Reservation, obj objectstore.Object) (artifactjson.Ref, error) {
	if r == nil || r.db == nil || scope != r.scope || reservation.ArtifactID == uuid.Nil ||
		reservation.Token == uuid.Nil || obj.SHA256 != r.sha ||
		obj.Bytes != int64(len(r.params)) || obj.ContentType != "application/json" ||
		obj.VersionID == "" || obj.VersionID == "null" {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	key, err := artifactjson.DocumentKey(scope, reservation.ArtifactID)
	if err != nil || obj.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var completed bool
	err = withPerspectivePreviewDocumentTx(ctx, r.db, scope.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"SELECT photo.complete_v28_restoration_preview_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)",
			scope.Tenant, scope.Run, scope.Owner, reservation.ArtifactID,
			reservation.Token, obj.SHA256, obj.Bytes, r.bucket, obj.VersionID,
			r.side, r.variant, r.image).Scan(&completed)
	})
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !completed {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: reservation.ArtifactID, SHA256: obj.SHA256}, nil
}

// PublishRestorationPreviewTasks may resume partially published documents.
// Its SQL reservation and completion repeat live source and child-pin checks.
func PublishRestorationPreviewTasks(ctx context.Context, db *pgxpool.Pool,
	objects artifactjson.Objects, bucket string, plan RestorationPreviewPlan,
	baseRaw, candidateRaw []byte) ([]PreparedTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRestorationPreviewIntent(plan); err != nil {
		return nil, err
	}
	if db == nil || objects == nil {
		return nil, v2input.ErrInvalid
	}
	rebuilt, err := CompileRestorationPreviewPlan(plan.source, plan.srgb, baseRaw, candidateRaw)
	if err != nil {
		return nil, err
	}
	for i := range plan.docs {
		if plan.docs[i].Side != rebuilt.docs[i].Side ||
			plan.docs[i].ID != rebuilt.docs[i].ID ||
			!bytes.Equal(plan.docs[i].Params, rebuilt.docs[i].Params) ||
			!bytes.Equal(plan.docs[i].Profile, rebuilt.docs[i].Profile) ||
			!restorationPreviewInputsEqual(plan.docs[i].Inputs, rebuilt.docs[i].Inputs) {
			return nil, v2input.ErrInvalid
		}
	}
	docs := rebuilt.Documents()
	repos := make([]*RestorationPreviewDocuments, len(docs))
	for i, doc := range docs {
		image := baseRaw
		if doc.Side == "after" {
			image = candidateRaw
		}
		repos[i], err = NewRestorationPreviewDocuments(db, plan.source.Tenant,
			bucket, doc, image)
		if err != nil {
			return nil, err
		}
	}
	tasks := make([]PreparedTask, 0, len(docs))
	for i, doc := range docs {
		repo := repos[i]
		ref, err := (artifactjson.Publisher{Repository: repo, Objects: objects}).Publish(ctx,
			repo.scope, []byte(repo.params))
		if err != nil {
			return nil, err
		}
		task, err := PrepareRestorationPreviewTask(doc,
			review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256})
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func restorationPreviewInputsEqual(a, b []review.ArtifactRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
