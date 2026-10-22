package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

// NewSQLShownOperationResolver selects the operation from authenticated current
// review documents. Clients cannot choose a resolver or downgrade shown Apply.
// Each selected resolver independently repeats scope and physical verification.
func NewSQLShownOperationResolver(db *pgxpool.Pool, read PlanObjectReader, budget int64, modelSHA, datasetSHA string,
	exposure, removal ShownApplyResolver, cleanup ...ShownApplyResolver) (ShownApplyResolver, error) {
	if len(cleanup) > 2 {
		return nil, ErrInvalid
	}
	var backgroundCleanup, retouch ShownApplyResolver
	if len(cleanup) >= 1 {
		backgroundCleanup = cleanup[0]
	}
	if len(cleanup) == 2 {
		retouch = cleanup[1]
	}
	if db == nil || read == nil || budget < 1 || budget > 64*1048576 || !shaPattern.MatchString(modelSHA) || !shaPattern.MatchString(datasetSHA) || (exposure == nil && removal == nil && backgroundCleanup == nil && retouch == nil) {
		return nil, ErrInvalid
	}
	return func(ctx context.Context, who auth.Identity, tenant, intent, child uuid.UUID, request ApplyDecision) (ShownApplyResolution, error) {
		empty := ShownApplyResolution{}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if child == uuid.Nil {
			return empty, ErrInvalid
		}
		raw, err := canonical.Marshal(request)
		if err != nil {
			return empty, ErrInvalid
		}
		owned, err := DecodeApplyDecision(raw)
		if err != nil {
			return empty, err
		}
		review, _, err := PrepareScopedApplyReview(ctx, db, who, tenant, intent, owned, read, budget, modelSHA, datasetSHA)
		if err != nil {
			return empty, err
		}
		selected, err := selectShownOperationResolver(review.Intent.Kind, exposure, removal, backgroundCleanup, retouch)
		if err != nil {
			return empty, err
		}
		owned, err = DecodeApplyDecision(raw)
		if err != nil {
			return empty, err
		}
		result, err := selected(ctx, who, tenant, intent, child, owned)
		if err != nil {
			return empty, err
		}
		// A replacement of the intent between routing and the selected reader may
		// not redirect the request into another operation family.
		if result.Review.Intent.Kind != review.Intent.Kind {
			return empty, ErrConflict
		}
		return result, nil
	}, nil
}

func selectShownOperationResolver(kind string, exposure, removal ShownApplyResolver, cleanup ...ShownApplyResolver) (ShownApplyResolver, error) {
	if len(cleanup) > 2 {
		return nil, ErrInvalid
	}
	var selected ShownApplyResolver
	switch kind {
	case "local_exposure":
		selected = exposure
	case "remove_object":
		selected = removal
	case "background_cleanup":
		if len(cleanup) >= 1 {
			selected = cleanup[0]
		}
	case "retouch_region":
		if len(cleanup) == 2 {
			selected = cleanup[1]
		}
	default:
		return nil, ErrInvalid
	}
	if selected == nil {
		return nil, ErrUnavailable
	}
	return selected, nil
}
