package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type approvedTransportRepository struct {
	db                            *pgxpool.Pool
	scope                         artifactjson.Scope
	intent, actor, session, child uuid.UUID
	revision                      int64
	target                        LockedTarget
	edit                          ApprovedEdit
	params                        v2input.Ref
	bucket                        string
}

type approvedExposurePublisher struct {
	repository approvedTransportRepository
	objects    artifactjson.Objects
	task       uuid.UUID
}

// NewApprovedExposurePublisher binds immutable compiler scope to the pre-child
// SQL publication contour. Every reservation/completion repeats authenticated
// review scope checks; final admission still verifies the actual closure.
func NewApprovedExposurePublisher(db *pgxpool.Pool, objects artifactjson.Objects, bucket string,
	tenant, intent, actor, session, parent, child uuid.UUID, revision int64, target LockedTarget, frozen FrozenExposureTask) (v2input.Publisher, error) {
	if _, err := frozen.DraftTask(); err != nil {
		return nil, err
	}
	return NewApprovedTransportPublisher(db, objects, bucket, tenant, intent, actor, session, parent, child, revision, target,
		ApprovedTaskDraft{TaskID: frozen.task, TargetID: frozen.target, Params: frozen.params, Body: frozen.closure.ParamsJCS, Kind: "local_exposure"})
}

// NewApprovedTransportPublisher supports transport for every approved semantic
// kind without selecting or enabling an operation. Admission independently
// recompiles the private review proof and qualifies the actual adapter.
func NewApprovedTransportPublisher(db *pgxpool.Pool, objects artifactjson.Objects, bucket string,
	tenant, intent, actor, session, parent, child uuid.UUID, revision int64, target LockedTarget, draft ApprovedTaskDraft) (v2input.Publisher, error) {
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || parent == uuid.Nil || child == uuid.Nil || child == parent || revision < 1 || target.ID != draft.TargetID || target.State != "proposed" || target.Revision < 1 || target.ChildRunID != nil || target.BatchID != nil {
		return nil, ErrInvalid
	}
	sum := sha256.Sum256(draft.Body)
	if len(draft.Body) < 2 || len(draft.Body) > 1048576 || hex.EncodeToString(sum[:]) != draft.Params.SHA256 || draft.Params.ArtifactID != uuid.NewSHA1(draft.TaskID, []byte("Params")) {
		return nil, ErrInvalid
	}
	var edit ApprovedEdit
	if decodeStrict(draft.Body, &edit) != nil || edit.TargetID != target.ID || edit.Kind != draft.Kind || !edit.PreservePersons || target.AssetVersionID != edit.Source.AssetVersionID || target.CandidateArtifactID == nil || *target.CandidateArtifactID != edit.Candidate.ArtifactID || draft.TaskID != uuid.NewSHA1(child, []byte("semantic.edit:"+target.ID.String())) {
		return nil, ErrInvalid
	}
	repo := approvedTransportRepository{db: db, scope: artifactjson.Scope{Tenant: tenant, Run: parent, Owner: draft.TaskID}, intent: intent, actor: actor, session: session, child: child, revision: revision, target: target, edit: edit, bucket: bucket}
	return approvedExposurePublisher{repository: repo, objects: objects, task: draft.TaskID}, nil
}

func (p approvedExposurePublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if task != p.task {
		return v2input.Ref{}, ErrInvalid
	}
	var purpose string
	switch contract {
	case "InputClosureChunk":
		if ordinal < 0 || ordinal >= v2input.MaxChunks {
			return v2input.Ref{}, ErrInvalid
		}
		purpose = fmt.Sprintf("v2_input_chunk_%d", ordinal)
	case "InputClosureManifest":
		if ordinal < 1 || ordinal > v2input.MaxChunks {
			return v2input.Ref{}, ErrInvalid
		}
		purpose = "v2_input_manifest"
	default:
		return v2input.Ref{}, ErrInvalid
	}
	repo := p.repository
	repo.scope.Purpose = purpose
	sum := sha256.Sum256(body)
	repo.params = v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(purpose)), SHA256: hex.EncodeToString(sum[:])}
	publisher := artifactjson.Publisher{Repository: repo, Objects: p.objects}
	ref, err := publisher.Publish(ctx, repo.scope, body)
	if err != nil {
		return v2input.Ref{}, err
	}
	if ref.ID != repo.params.ArtifactID || ref.SHA256 != repo.params.SHA256 {
		return v2input.Ref{}, ErrInvalid
	}
	return repo.params, nil
}

func (r approvedTransportRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if scope != r.scope || sha != r.params.SHA256 || !shaPattern.MatchString(sha) || size < 2 || size > 1<<20 {
		return artifactjson.Reservation{}, ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err := r.db.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_semantic_approved_transport_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, scope.Tenant, r.intent, r.actor, r.session, r.revision, r.target.ID, r.target.Revision, r.edit.Source.AssetVersionID, r.edit.Candidate.ArtifactID, r.edit.Candidate.SHA256, r.child, scope.Purpose, scope.Owner, r.params.ArtifactID, sha, size, r.bucket).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	switch outcome {
	case "conflict":
		return artifactjson.Reservation{}, artifactjson.ErrConflict
	case "stale", "forbidden":
		return artifactjson.Reservation{}, artifactjson.ErrStale
	case "quota":
		return artifactjson.Reservation{}, artifactjson.ErrQuota
	case "created", "resumed", "replayed":
	default:
		return artifactjson.Reservation{}, ErrInvalid
	}
	if id == nil || *id != r.params.ArtifactID || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return artifactjson.Reservation{}, ErrInvalid
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	return result, nil
}

func (r approvedTransportRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || scope != r.scope || ticket.ArtifactID != r.params.ArtifactID || ticket.Token == uuid.Nil || object.SHA256 != r.params.SHA256 || object.Key != key || !shaPattern.MatchString(object.SHA256) || object.Bytes < 2 || object.Bytes > 1<<20 || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return artifactjson.Ref{}, ErrInvalid
	}
	var done bool
	err = r.db.QueryRow(ctx, `SELECT photo.complete_semantic_approved_transport_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, scope.Tenant, r.intent, r.actor, r.session, r.revision, r.target.ID, r.target.Revision, r.edit.Source.AssetVersionID, r.edit.Candidate.ArtifactID, r.edit.Candidate.SHA256, r.child, scope.Purpose, scope.Owner, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.bucket, object.VersionID).Scan(&done)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !done {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}
