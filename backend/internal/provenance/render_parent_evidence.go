package provenance

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type SigningRenderParent struct {
	Root     v2input.Ref
	Producer CanonicalSigningTask
}
type SQLSigningRenderParentReader struct {
	DB       *pgxpool.Pool
	Adoption SigningRenderAdoptionReader
	Tasks    SigningTaskCatalog
}

// Registered ImageResult input order is the normative report-parent order.
// Missing/unadopted entries fail the entire read; none are silently filtered out.
func (s SQLSigningRenderParentReader) ReadSigningRenderParents(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) ([]SigningRenderParent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Adoption == nil || s.Tasks == nil {
		return nil, ErrInvalid
	}
	root, err := s.Adoption.ReadSigningAdoptedRenderRoot(ctx, who, ready, task)
	if err != nil {
		return nil, err
	}
	type pin struct {
		ref      v2input.Ref
		producer uuid.UUID
	}
	pins := []pin{}
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT i.artifact_id,i.sha256,a.created_by_task_id,
   coalesce(a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
    AND a.media_type='application/json' AND a.output_role='document' AND a.bytes BETWEEN 2 AND 1048576
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND p.state='succeeded' AND NOT p.cancel_requested AND p.id<>$2
    AND a.produced_attempt=p.attempt AND a.produced_fence=p.fencing_token
    AND z.attempt=p.attempt AND z.fencing_token=p.fencing_token
    AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
    AND EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.v2_artifact_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id AND l.project_id=w.project_id
     WHERE w.tenant_id=p.tenant_id AND w.run_id=p.run_id AND w.project_id=$3
     AND l.artifact_id=a.id AND l.sha256=a.sha256 AND l.ordinal=0
     AND l.purpose=encode(sha256(convert_to('tasks/'||p.id||'/$','UTF8')),'hex')),false)
   FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
   LEFT JOIN photo.tasks p ON p.tenant_id=a.tenant_id AND p.id=a.created_by_task_id
   LEFT JOIN photo.task_results z ON z.tenant_id=p.tenant_id AND z.task_id=p.id AND z.id=p.canonical_result_id
   WHERE i.tenant_id=$1 AND i.task_id=$2 AND i.project_id=$3 AND i.contract='ImageResult'
   ORDER BY i.ordinal,i.artifact_id LIMIT 34`, ready.Tenant, task.Task, ready.Project)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[uuid.UUID]bool{}
		for rows.Next() {
			var value pin
			var producer *uuid.UUID
			var accepted bool
			if err := rows.Scan(&value.ref.ArtifactID, &value.ref.SHA256, &producer, &accepted); err != nil {
				return err
			}
			if !accepted || producer == nil || *producer == uuid.Nil || !ref(value.ref) || seen[value.ref.ArtifactID] || len(pins) >= 33 {
				return ErrInvalid
			}
			value.producer = *producer
			seen[value.ref.ArtifactID] = true
			pins = append(pins, value)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	parents := make([]SigningRenderParent, 0, len(pins))
	for _, pin := range pins {
		producer, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, pin.producer)
		if err != nil {
			return nil, err
		}
		roots, err := canonicalTaskOutputRefs(producer.Outputs)
		if err != nil || len(roots) != 1 || roots[0] != pin.ref {
			return nil, ErrInvalid
		}
		parents = append(parents, SigningRenderParent{Root: pin.ref, Producer: producer})
	}
	current, err := s.Adoption.ReadSigningAdoptedRenderRoot(ctx, who, ready, task)
	if err != nil {
		return nil, err
	}
	if current != root {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return parents, nil
}
