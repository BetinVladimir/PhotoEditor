package provenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type SigningRenderImageProducerCatalog interface {
	ReadSigningRenderImageProducer(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (CanonicalSigningTask, error)
}
type BoundSigningRenderImageProducer struct {
	Catalog   SigningRenderImageProducerCatalog
	Adoption  SigningRenderAdoptionReader
	Documents SigningTaskDocumentReader
}

func (s BoundSigningRenderImageProducer) ResolveSigningImageProducer(ctx context.Context, who auth.Identity, ready ReadySigningExport, image v2input.Ref) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if s.Catalog == nil || s.Adoption == nil || s.Documents == nil || !ref(image) {
		return uuid.Nil, ErrInvalid
	}
	task, err := s.Catalog.ReadSigningRenderImageProducer(ctx, who, ready, image)
	if err != nil {
		return uuid.Nil, err
	}
	task, err = ownSigningProducerTask(task)
	if err != nil {
		return uuid.Nil, err
	}
	if task.Task == ready.Task || task.Operation != "render.compose" || task.Version != "1" {
		return uuid.Nil, ErrInvalid
	}
	_, result, err := ReadSigningRenderResult(ctx, s.Adoption, s.Documents, who, ready, task)
	if err != nil {
		return uuid.Nil, err
	}
	if result.WorkingImage != image {
		return uuid.Nil, ErrInvalid
	}
	current, err := s.Catalog.ReadSigningRenderImageProducer(ctx, who, ready, image)
	if err != nil {
		return uuid.Nil, err
	}
	if !sameSigningProducerTask(task, current) {
		return uuid.Nil, ErrInvalid
	}
	return task.Task, ctx.Err()
}

// Only a genuine no-row from the strict direct-output path permits fallback.
// Integrity/authentication errors are never hidden by alternate resolution.
type SigningImageProducerWithRender struct{ Direct, Render SigningImageProducer }

func (s SigningImageProducerWithRender) ResolveSigningImageProducer(ctx context.Context, who auth.Identity, ready ReadySigningExport, image v2input.Ref) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if s.Direct == nil || s.Render == nil {
		return uuid.Nil, ErrInvalid
	}
	id, err := s.Direct.ResolveSigningImageProducer(ctx, who, ready, image)
	if !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	return s.Render.ResolveSigningImageProducer(ctx, who, ready, image)
}

type SQLSigningRenderImageProducerCatalog struct {
	DB    *pgxpool.Pool
	Tasks SigningTaskCatalog
}

func (s SQLSigningRenderImageProducerCatalog) ReadSigningRenderImageProducer(ctx context.Context, who auth.Identity, ready ReadySigningExport, image v2input.Ref) (CanonicalSigningTask, error) {
	var zero CanonicalSigningTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Tasks == nil || !ref(image) || ready.Tenant == uuid.Nil {
		return zero, ErrInvalid
	}
	var producer uuid.UUID
	var attempt int
	var fence int64
	err := tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, signingRenderImageProducerSQL, ready.Tenant, image.ArtifactID, image.SHA256).Scan(&producer, &attempt, &fence)
	})
	if err != nil {
		return zero, err
	}
	task, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, producer)
	if err != nil {
		return zero, err
	}
	if task.Task != producer || task.Attempt != attempt || task.Fence != fence || task.Operation != "render.compose" || task.Version != "1" || normalizeCanonicalSigningTask(&task) != nil {
		return zero, ErrInvalid
	}
	return task, nil
}

const signingRenderImageProducerSQL = `SELECT a.created_by_task_id,a.produced_attempt,a.produced_fence
 FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 WHERE a.tenant_id=$1 AND a.id=$2 AND a.sha256=$3 AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576 AND a.output_role='document'
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND t.operation='render.compose' AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
 AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token FOR SHARE OF a,t,z`

var _ SigningImageProducer = BoundSigningRenderImageProducer{}
var _ SigningImageProducer = SigningImageProducerWithRender{}
