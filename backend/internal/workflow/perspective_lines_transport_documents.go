package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type perspectiveLinesTransportDocuments struct {
	db      *pgxpool.Pool
	objects artifactjson.Objects
	bucket  string
	plan    PerspectiveLinesPlan
	params  review.ArtifactRef
}

func (d *perspectiveLinesTransportDocuments) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	if err := ctx.Err(); err != nil {
		return artifactjson.Ref{}, err
	}
	if d == nil || scope.Tenant != d.plan.scope.Tenant || scope.Run != d.plan.scope.Run || scope.Owner != d.plan.task || scope.AssemblyLease != uuid.Nil || scope.LocalPortrait || scope.DonorPortrait {
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	contract := "InputClosureChunk"
	ordinal := 0
	var chunk *v2input.Ref
	switch scope.Purpose {
	case "v2_input_chunk_0":
	case "v2_input_manifest":
		contract = "InputClosureManifest"
		ordinal = 1
		var value v2input.Manifest
		if len(body) > 65536 || json.Unmarshal(body, &value) != nil || len(value.Chunks) != 1 {
			return artifactjson.Ref{}, v2input.ErrInvalid
		}
		chunk = &value.Chunks[0]
	default:
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	doc, err := CompilePerspectiveLinesTransportDocument(d.plan, d.params, ordinal, contract, body, chunk)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if doc.Scope() != scope {
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	repository, err := newPerspectiveLinesTransportRepository(d.db, d.bucket, doc)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	return (artifactjson.Publisher{Repository: repository, Objects: d.objects}).Publish(ctx, scope, doc.Bytes())
}

// SQL reserves/completes each control document; blocked-task closure admission
// and source-object IO witnesses remain separate and are not performed here.
func PublishPerspectiveLinesClosure(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, plan PerspectiveLinesPlan, params review.ArtifactRef) (PerspectiveLinesClosure, error) {
	var zero PerspectiveLinesClosure
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return zero, objectstore.ErrInvalid
	}
	if _, err := PreparePerspectiveLinesTask(plan, params); err != nil {
		return zero, err
	}
	owned, err := CompilePerspectiveLinesPlan(plan.scope, plan.image, plan.workingICC)
	if err != nil {
		return zero, err
	}
	docs := &perspectiveLinesTransportDocuments{db: db, objects: objects, bucket: bucket, plan: owned, params: params}
	publisher, err := NewPerspectiveLinesTransportPublisher(owned, params, docs)
	if err != nil {
		return zero, err
	}
	return BuildPerspectiveLinesClosure(ctx, owned, params, publisher)
}
