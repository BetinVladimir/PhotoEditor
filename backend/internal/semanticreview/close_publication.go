package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

type PublishedClose struct {
	BatchID uuid.UUID
	Ref     v2input.Ref
	Proof   CloseProof
}

// PublishClose uses the existing versioned document publisher. Reservation and
// completion commit separately from IO; closure happens only in CommitClose.
func PublishClose(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ReviewClose) (PublishedClose, error) {
	if err := ctx.Err(); err != nil {
		return PublishedClose{}, err
	}
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return PublishedClose{}, ErrInvalid
	}
	proof, err := BuildCloseProof(prepared.Sources.RunID, request, prepared.PlanRef.SHA256, strconv.FormatInt(prepared.Sources.Revision, 10), "waiting_review", targets)
	if err != nil {
		return PublishedClose{}, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return PublishedClose{}, err
	}
	defer tx.Rollback(context.Background())
	identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, actor, session)
	if err != nil {
		return PublishedClose{}, err
	}
	if identity.RunID != prepared.Sources.RunID {
		return PublishedClose{}, ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return PublishedClose{}, err
	}
	owner := decisionDocumentOwner(tenant, identity.RunID, actor, proof.RequestSHA256)
	scope := artifactjson.Scope{Tenant: tenant, Run: identity.RunID, Owner: owner, Purpose: "review_selection"}
	repo := decisionDocumentRepository{db: db, scope: scope, intent: intent, actor: actor, session: session, revision: prepared.Sources.Revision, requestSHA: proof.RequestSHA256, bucket: bucket}
	publisher := artifactjson.Publisher{Repository: repo, Objects: objects}
	return publishCloseProof(ctx, owner, proof, func(ctx context.Context, body []byte) (v2input.Ref, error) {
        ref, err := publisher.Publish(ctx, scope, body)
        return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, err
    })
}

// Freeze proof bytes across publication waits, including a ready-document replay.
// The SQL reserve/complete repository remains the authenticated production path.
func publishCloseProof(ctx context.Context, owner uuid.UUID, proof CloseProof,
    publish func(context.Context, []byte) (v2input.Ref, error)) (PublishedClose, error) {
    empty := PublishedClose{}
    if err := ctx.Err(); err != nil { return empty, err }
    if owner == uuid.Nil || publish == nil || len(proof.Body) < 2 || !shaPattern.MatchString(proof.RequestSHA256) {
        return empty, ErrInvalid
    }
    proof.Body = append([]byte(nil), proof.Body...)
    digest := sha256.Sum256(proof.Body)
    if hex.EncodeToString(digest[:]) != proof.SHA256 { return empty, ErrInvalid }
    ref, err := publish(ctx, append([]byte(nil), proof.Body...))
    if err != nil { return empty, err }
    if err := ctx.Err(); err != nil { return empty, err }
    if ref.ArtifactID == uuid.Nil || ref.SHA256 != proof.SHA256 { return empty, ErrInvalid }
    return PublishedClose{BatchID: owner, Ref: ref, Proof: proof}, nil
}
