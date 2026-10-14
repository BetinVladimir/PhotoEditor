package provenance

import (
	"bytes"
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// A canonical succeeded result is not enough: the root must also have its
// deterministic adoption link in the same project/workflow and producer fence.
type SQLSigningRenderAdoptionReader struct {
	DB    *pgxpool.Pool
	Tasks SigningTaskCatalog
}

func (s SQLSigningRenderAdoptionReader) ReadSigningAdoptedRenderRoot(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Tasks == nil || task.Operation != "render.compose" || task.Version != "1" || normalizeCanonicalSigningTask(&task) != nil {
		return zero, ErrInvalid
	}
	roots, err := canonicalTaskOutputRefs(task.Outputs)
	if err != nil || len(roots) != 1 {
		return zero, ErrInvalid
	}
	root := roots[0]
	check := func() error {
		current, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, task.Task)
		if err != nil {
			return err
		}
		a, err := canonical.Marshal(current)
		if err != nil {
			return err
		}
		b, err := canonical.Marshal(task)
		if err != nil {
			return err
		}
		if !bytes.Equal(a, b) {
			return ErrInvalid
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return zero, err
	}
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var accepted bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.tasks t
   JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
   JOIN photo.v2_artifact_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id AND l.project_id=w.project_id
   JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
   WHERE t.tenant_id=$1 AND t.id=$2 AND t.run_id=$3 AND t.canonical_result_id=$4
   AND t.attempt=$5 AND t.fencing_token=$6 AND t.state='succeeded' AND NOT t.cancel_requested
   AND t.operation='render.compose' AND t.operation_version='1' AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
   AND w.project_id=$7 AND l.artifact_id=$8 AND l.sha256=$9 AND l.ordinal=0
   AND l.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
   AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
   AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
   AND a.output_role='document' AND a.bytes BETWEEN 2 AND 1048576 AND coalesce(a.object_version,'') NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))`, ready.Tenant, task.Task, task.Run, task.Result, task.Attempt, task.Fence, ready.Project, root.ArtifactID, root.SHA256).Scan(&accepted)
		if err != nil {
			return err
		}
		if !accepted {
			return ErrInvalid
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	if err := check(); err != nil {
		return zero, err
	}
	return root, nil
}
