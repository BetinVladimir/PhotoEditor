package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

type ReviewKindReader func(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (string, error)

// SQLReviewKindReader reads only the intent authorized by the current session.
// A missing retained object is unavailable, never an inferred semantic kind.
func SQLReviewKindReader(db *pgxpool.Pool, read PlanObjectReader) (ReviewKindReader, error) {
	if db == nil || read == nil {
		return nil, ErrInvalid
	}
	return func(ctx context.Context, who auth.Identity, tenant, intent uuid.UUID) (string, error) {
		if tenant == uuid.Nil || intent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
			return "", ErrInvalid
		}
		load := func() (PlanSource, error) {
			var source PlanSource
			source.Contract = "EditIntent"
			err := db.QueryRow(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_review_intent_object($1,$2,$3,$4)", tenant, intent, who.UserID, who.SessionID).Scan(&source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bucket, &source.Key, &source.Version, &source.Bytes, &source.MediaType)
			if errors.Is(err, pgx.ErrNoRows) {
				return source, ErrUnavailable
			}
			return source, err
		}
		return readVerifiedSemanticKind(ctx, load, read)
	}, nil
}
