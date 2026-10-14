package provenance

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
)

// The router never retries failed render authority through another operation.
type RenderPresentationSigningTaskResolver struct {
	Presentation SigningTaskLineageResolver
	Render       SigningTaskLineageResolver
}

func (s RenderPresentationSigningTaskResolver) ResolveSigningTaskLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (ResolvedSigningTask, error) {
	if err := ctx.Err(); err != nil {
		return ResolvedSigningTask{}, err
	}
	resolver := s.Presentation
	if task.Operation == "render.compose" {
		resolver = s.Render
	}
	if resolver == nil {
		return ResolvedSigningTask{}, ErrInvalid
	}
	return resolver.ResolveSigningTaskLineage(ctx, who, ready, task)
}

// NewSQLRenderPresentationSigningExecution adds fully read render evidence to
// presentation ancestry. Unknown dependency contracts and other unsupported
// imaging operations fail explicitly. No native signer is installed here.
func NewSQLRenderPresentationSigningExecution(db *pgxpool.Pool, bucket string, objects SigningObjects, workICCSHA string) (SigningExecutionSource, error) {
	presentation, err := NewSQLPresentationSigningExecution(db, bucket, objects, workICCSHA)
	if err != nil {
		return nil, err
	}
	bound, ok := presentation.(BoundSigningExecutionSource)
	if !ok {
		return nil, ErrInvalid
	}
	reviewed, err := NewSQLSigningRenderReviewedCausalSource(db, bucket, bound.Tasks, objects)
	if err != nil {
		return nil, err
	}
	render := RenderSigningTaskResolver{Reviewed: reviewed, Dependencies: BoundSigningRenderDependencyResolver{Causal: reviewed.Causal}}
	bound.Claims = RenderPresentationSigningTaskResolver{Presentation: bound.Claims, Render: render}
	return bound, nil
}

var _ SigningTaskLineageResolver = RenderPresentationSigningTaskResolver{}

// NewSQLRenderPresentationSigningExportSource connects the render-capable graph
// to the same reserved immutable lineage publication protocol.
func NewSQLRenderPresentationSigningExportSource(db *pgxpool.Pool, bucket string, reads SigningObjects, writes artifactjson.Objects, workICCSHA string) (SigningExportSource, error) {
	if writes == nil {
		return nil, ErrInvalid
	}
	execution, err := NewSQLRenderPresentationSigningExecution(db, bucket, reads, workICCSHA)
	if err != nil {
		return nil, err
	}
	exports := SQLSigningExportCatalog{DB: db, Bucket: bucket}
	publisher := ImmutableSigningLineagePublisher{Catalog: exports, Store: SQLSigningLineageStore{DB: db, Bucket: bucket}, Objects: writes}
	return BoundSigningExportSource{Catalog: exports, Lineages: CompiledExportLineageSource{Execution: execution, Documents: publisher}}, nil
}
