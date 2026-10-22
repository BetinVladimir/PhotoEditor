package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type replacementDetectionTransportRepository struct {
	source   ReplacementDetectionDocuments
	document PerspectiveLinesTransportDocument
}

var _ artifactjson.Repository = (*replacementDetectionTransportRepository)(nil)

func (r *replacementDetectionTransportRepository) validate(ctx context.Context, s artifactjson.Scope, sha string, size int64) error {
	if r == nil {
		return v2input.ErrInvalid
	}
	scope, body, task, _, err := r.source.Plan.PreparePublication(ctx)
	if err != nil {
		return err
	}
	if _, err = r.source.validate(ctx, scope, task.Params.SHA256, int64(len(body))); err != nil {
		return err
	}
	if s != r.document.Scope() || s.Tenant != scope.Tenant || s.Run != scope.Run || s.Owner != scope.Owner || sha != r.document.SHA256() || size != int64(len(r.document.Bytes())) {
		return v2input.ErrInvalid
	}
	_, err = r.document.Proof()
	return err
}
func (r *replacementDetectionTransportRepository) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var result artifactjson.Reservation
	if err := r.validate(ctx, s, sha, size); err != nil {
		return result, err
	}
	err := r.source.transaction(ctx, s, func(ctx context.Context, tx pgx.Tx, proof []byte) error {
		var id, token uuid.UUID
		var ready bool
		err := tx.QueryRow(ctx, "SELECT artifact_id,token,ready FROM photo.reserve_v29_replacement_detector_transport($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)", s.Tenant, s.Run, s.Owner, s.Purpose, sha, size, r.source.Bucket, string(r.document.Bytes()), string(proof)).Scan(&id, &token, &ready)
		if errors.Is(err, pgx.ErrNoRows) {
			return artifactjson.ErrStale
		}
		if err != nil {
			return err
		}
		if id == uuid.Nil || token == uuid.Nil {
			return objectstore.ErrIntegrity
		}
		result = artifactjson.Reservation{ArtifactID: id, Token: token}
		if ready {
			result.Ready = &artifactjson.Ref{ID: id, SHA256: sha}
		}
		return nil
	})
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	return result, nil
}
func (r *replacementDetectionTransportRepository) Complete(ctx context.Context, s artifactjson.Scope, ticket artifactjson.Reservation, obj objectstore.Object) (artifactjson.Ref, error) {
	if err := r.validate(ctx, s, obj.SHA256, obj.Bytes); err != nil {
		return artifactjson.Ref{}, err
	}
	key, err := artifactjson.DocumentKey(s, ticket.ArtifactID)
	if err != nil || ticket.ArtifactID == uuid.Nil || ticket.Token == uuid.Nil || obj.Key != key || obj.ContentType != "application/json" || obj.VersionID == "" || obj.VersionID == "null" || len(obj.VersionID) > 1024 {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	err = r.source.transaction(ctx, s, func(ctx context.Context, tx pgx.Tx, proof []byte) error {
		var accepted bool
		err := tx.QueryRow(ctx, "SELECT photo.complete_v29_replacement_detector_transport($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)", s.Tenant, s.Run, s.Owner, s.Purpose, ticket.ArtifactID, ticket.Token, obj.SHA256, obj.Bytes, r.source.Bucket, obj.VersionID, string(r.document.Bytes()), string(proof)).Scan(&accepted)
		if err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
	if err != nil {
		return artifactjson.Ref{}, err
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: obj.SHA256}, nil
}

type replacementDetectionTransportDocuments struct {
	source  ReplacementDetectionDocuments
	objects artifactjson.Objects
}

func (d replacementDetectionTransportDocuments) Publish(ctx context.Context, s artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	scope, params, task, _, err := d.source.Plan.PreparePublication(ctx)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if _, err = d.source.validate(ctx, scope, task.Params.SHA256, int64(len(params))); err != nil {
		return artifactjson.Ref{}, err
	}
	if d.objects == nil {
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	ordinal, contract := 0, "InputClosureChunk"
	var chunk *v2input.Ref
	switch s.Purpose {
	case "v2_input_chunk_0":
	case "v2_input_manifest":
		ordinal, contract = 1, "InputClosureManifest"
		var manifest v2input.Manifest
		if len(body) > 65536 || json.Unmarshal(body, &manifest) != nil || len(manifest.Chunks) != 1 {
			return artifactjson.Ref{}, v2input.ErrInvalid
		}
		chunk = &manifest.Chunks[0]
	default:
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	doc, err := CompilePerspectiveLinesTransportDocument(d.source.Plan.detector, task.Params, ordinal, contract, body, chunk)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if doc.Scope() != s {
		return artifactjson.Ref{}, v2input.ErrInvalid
	}
	repository := &replacementDetectionTransportRepository{source: d.source, document: doc}
	return (artifactjson.Publisher{Repository: repository, Objects: d.objects}).Publish(ctx, s, doc.Bytes())
}

// PublishInputClosure requires Params and the blocked task to have been saved.
// Each document transaction repeats current physical source and review locks.
func (r ReplacementDetectionDocuments) PublishInputClosure(ctx context.Context, objects artifactjson.Objects) (PerspectiveLinesClosure, error) {
	scope, body, task, _, err := r.Plan.PreparePublication(ctx)
	if err != nil {
		return PerspectiveLinesClosure{}, err
	}
	if _, err = r.validate(ctx, scope, task.Params.SHA256, int64(len(body))); err != nil {
		return PerspectiveLinesClosure{}, err
	}
	if objects == nil {
		return PerspectiveLinesClosure{}, v2input.ErrInvalid
	}
	publisher, err := NewPerspectiveLinesTransportPublisher(r.Plan.detector, task.Params, replacementDetectionTransportDocuments{source: r, objects: objects})
	if err != nil {
		return PerspectiveLinesClosure{}, err
	}
	return r.Plan.BuildInputClosure(ctx, task.Params, publisher)
}
