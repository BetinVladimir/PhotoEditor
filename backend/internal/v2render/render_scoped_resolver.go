package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// SQLRenderInputResolver creates a new asset-scoped catalog for every request.
// A batch coordinator may reuse the resolver across parts, but never a catalog
// bound to the first photograph. The API pool preserves artifact RLS; writes
// and transport registration continue to use the separate scheduler pool.
type SQLRenderInputResolver struct {
	API       *pgxpool.Pool
	ProjectID uuid.UUID
	Bucket    string
	Objects   v2input.SessionAnalysisObjectReader
}

func (s SQLRenderInputResolver) ResolveTaskInputs(ctx context.Context, who auth.Identity, tenant uuid.UUID, roots []v2input.Entry, binding v2input.SessionVersionBinding) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.API == nil || s.Objects == nil || s.ProjectID == uuid.Nil || s.Bucket == "" || tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	resolver := v2draft.ClosureResolver{Catalog: v2draft.SQLArtifactCatalog{DB: s.API, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}, Objects: s.Objects}
	return resolver.ResolveTaskInputs(ctx, who, tenant, append([]v2input.Entry(nil), roots...), binding)
}

var _ CompositionInputResolver = SQLRenderInputResolver{}
