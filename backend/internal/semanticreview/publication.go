package semanticreview

import (
	"context"
	"crypto/sha256"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type PublishedDecision struct {
	BatchID uuid.UUID
	Ref     v2input.Ref
	Proof   DecisionProof
}

type decisionDocumentRepository struct {
	db                     *pgxpool.Pool
	scope                  artifactjson.Scope
	intent, actor, session uuid.UUID
	revision               int64
	requestSHA, bucket     string
}

func decisionDocumentOwner(tenant, run, actor uuid.UUID, requestSHA string) uuid.UUID {
	sum := sha256.Sum256([]byte("v27.review/" + tenant.String() + "/" + run.String() + "/" + actor.String() + "/" + requestSHA))
	id, _ := uuid.FromBytes(sum[:16])
	return id
}

// PublishDecision rebuilds the proof, resolves authenticated identity, and
// commits reservation/completion separately from object IO. It does not save a
// batch or admit Apply; the final decision commit must revalidate source pins.
func PublishDecision(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision) (PublishedDecision, error) {
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return PublishedDecision{}, ErrInvalid
	}
	proof, err := BuildDecisionProof(prepared, targets, request)
	if err != nil {
		return PublishedDecision{}, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return PublishedDecision{}, err
	}
	defer tx.Rollback(ctx)
	identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, actor, session)
	if err != nil {
		return PublishedDecision{}, err
	}
	if identity.RunID != prepared.Sources.RunID {
		return PublishedDecision{}, ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return PublishedDecision{}, err
	}
	owner := decisionDocumentOwner(tenant, identity.RunID, actor, proof.RequestSHA256)
	scope := artifactjson.Scope{Tenant: tenant, Run: identity.RunID, Owner: owner, Purpose: "review_selection"}
	repo := decisionDocumentRepository{db: db, scope: scope, intent: intent, actor: actor, session: session, revision: prepared.Sources.Revision, requestSHA: proof.RequestSHA256, bucket: bucket}
	publisher := artifactjson.Publisher{Repository: repo, Objects: objects}
	ref, err := publisher.Publish(ctx, scope, proof.Body)
	if err != nil {
		return PublishedDecision{}, err
	}
	if ref.SHA256 != proof.SHA256 {
		return PublishedDecision{}, ErrInvalid
	}
	return PublishedDecision{BatchID: owner, Ref: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Proof: proof}, nil
}

func (r decisionDocumentRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if scope != r.scope || !shaPattern.MatchString(sha) || size < 2 || size > 1<<20 {
		return artifactjson.Reservation{}, ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err := r.db.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_semantic_decision_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`, scope.Tenant, r.intent, r.actor, r.session, r.revision, r.requestSHA, sha, size, r.bucket).Scan(&outcome, &id, &token, &ready)
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
	if id == nil || *id == uuid.Nil || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return artifactjson.Reservation{}, ErrInvalid
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	return result, nil
}

func (r decisionDocumentRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || scope != r.scope || ticket.Token == uuid.Nil || object.Key != key || !shaPattern.MatchString(object.SHA256) || object.Bytes < 2 || object.Bytes > 1<<20 || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return artifactjson.Ref{}, ErrInvalid
	}
	var done bool
	err = r.db.QueryRow(ctx, `SELECT photo.complete_semantic_decision_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, scope.Tenant, r.intent, r.actor, r.session, r.revision, r.requestSHA, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.bucket, object.VersionID).Scan(&done)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !done {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}
