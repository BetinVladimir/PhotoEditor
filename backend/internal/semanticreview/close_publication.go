package semanticreview

import (
	"context"
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
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
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
	defer tx.Rollback(ctx)
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
	ref, err := publisher.Publish(ctx, scope, proof.Body)
	if err != nil {
		return PublishedClose{}, err
	}
	if ref.SHA256 != proof.SHA256 {
		return PublishedClose{}, ErrInvalid
	}
	return PublishedClose{BatchID: owner, Ref: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Proof: proof}, nil
}
