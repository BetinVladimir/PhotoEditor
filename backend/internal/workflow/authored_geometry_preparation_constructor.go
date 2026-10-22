package workflow

import (
	"context"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"strings"
)

func ownAuthoredGeometryInventory(ctx context.Context, tenants []uuid.UUID, generations map[uuid.UUID]v2input.Ref) ([]uuid.UUID, map[uuid.UUID]v2input.Ref, error) {
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	if len(tenants) == 0 || len(tenants) > 10000 || len(generations) != len(tenants) {
		return nil, nil, v2input.ErrInvalid
	}
	owned := make(map[uuid.UUID]v2input.Ref, len(tenants))
	for _, tenant := range tenants {
		if e := ctx.Err(); e != nil {
			return nil, nil, e
		}
		ref, ok := generations[tenant]
		decoded, e := hex.DecodeString(ref.SHA256)
		if tenant == uuid.Nil || !ok || ref.ArtifactID == uuid.Nil || e != nil || len(decoded) != 32 || len(ref.SHA256) != 64 || ref.SHA256 != strings.ToLower(ref.SHA256) {
			return nil, nil, v2input.ErrInvalid
		}
		if _, exists := owned[tenant]; exists {
			return nil, nil, v2input.ErrInvalid
		}
		owned[tenant] = ref
	}
	return append([]uuid.UUID(nil), tenants...), owned, ctx.Err()
}

// NewAuthoredGeometryPreparationProcessor connects the real native deployment
// reader and producer to the blend producer and fair scheduler processor. It
// performs no SQL, publishes no qualification, and starts no goroutine. Caller
// must have verified the genuine installed public chain before scheduling this
// processor. Configured startup intentionally retains its capture-required
// error until that actual owning installation gate exists; no bool/callback
// here can override it. Generation refs are merely operator selections: the
// real SQLAuthoredGeometryGenerationReader verifies each on every Prepare.
func NewAuthoredGeometryPreparationProcessor(ctx context.Context, scheduler, api *pgxpool.Pool, objects v2input.SessionAnalysisObjectReader, publisher artifactjson.Objects, bucket, nativePath, nativeSHA string, tenants []uuid.UUID, generations map[uuid.UUID]v2input.Ref) (*AuthoredGeometryPreparationProcessor, error) {
	ownedTenants, owned, e := ownAuthoredGeometryInventory(ctx, tenants, generations)
	if e != nil {
		return nil, e
	}
	native, e := v2render.NewSQLAuthoredNativePreparation(ctx, scheduler, api, objects, publisher, bucket, nativePath, nativeSHA)
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	preparation := v2render.SQLAuthoredGeometryPreparation{Scheduler: scheduler, API: api, Objects: objects, PublisherObjects: publisher, Bucket: bucket, Native: native}
	return &AuthoredGeometryPreparationProcessor{DB: scheduler, Preparation: preparation, generations: owned, tenants: ownedTenants, after: map[uuid.UUID]uuid.UUID{}}, nil
}
