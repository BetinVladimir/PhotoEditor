package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

type PreviewKindResolver func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (string, error)
type PreviewReader interface {
	ReadPreview(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (Preview, error)
}
type PreviewOperationRouter struct {
	Resolve           PreviewKindResolver
	Exposure, Removal PreviewReader
	BackgroundCleanup PreviewReader
	Retouch           PreviewReader
}

func (s *PreviewOperationRouter) ReadPreview(ctx context.Context, who auth.Identity, tenant, intent, target uuid.UUID, request PreviewRequest) (Preview, error) {
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	if s == nil || s.Resolve == nil {
		return Preview{}, ErrUnavailable
	}
	kind, err := s.Resolve(ctx, who, tenant, intent, target, request)
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	if err != nil {
		return Preview{}, err
	}
	var provider PreviewReader
	switch kind {
	case "local_exposure":
		provider = s.Exposure
	case "remove_object":
		provider = s.Removal
	case "background_cleanup":
		provider = s.BackgroundCleanup
	case "retouch_region":
		provider = s.Retouch
	default:
		return Preview{}, ErrInvalid
	}
	if provider == nil {
		return Preview{}, ErrUnavailable
	}
	return provider.ReadPreview(ctx, who, tenant, intent, target, request)
}

// SQLPreviewKindResolver owns exact-version intent bytes and rechecks the same
// metadata after IO. Dispatch has no approval or preview creation authority.
func SQLPreviewKindResolver(db *pgxpool.Pool, read PlanObjectReader) (PreviewKindResolver, error) {
	if db == nil || read == nil {
		return nil, ErrInvalid
	}
	return func(ctx context.Context, who auth.Identity, tenant, intent, target uuid.UUID, request PreviewRequest) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if request.Validate() != nil || tenant == uuid.Nil || intent == uuid.Nil || target == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
			return "", ErrInvalid
		}
		load := func() (PlanSource, error) {
			var source PlanSource
			err := db.QueryRow(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_preview_intent_object($1,$2,$3,$4,$5,$6,$7,$8,$9)", tenant, intent, target, who.UserID, who.SessionID, request.ExpectedRevision, request.TargetRevision, request.PlanSHA, request.CandidateSHA).Scan(&source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bucket, &source.Key, &source.Version, &source.Bytes, &source.MediaType)
			if errors.Is(err, pgx.ErrNoRows) {
				return source, ErrConflict
			}
			return source, err
		}
		return readVerifiedSemanticKind(ctx, load, read)
	}, nil
}
