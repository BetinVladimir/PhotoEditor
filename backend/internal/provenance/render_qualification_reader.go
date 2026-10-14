package provenance

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
)

type SQLSigningRenderQualificationReader struct {
	DB       *pgxpool.Pool
	Adoption SigningRenderAdoptionReader
}

// ReadSigningRenderQualification uses the immutable historical run pin, not
// the live execution-admission helper, which intentionally rejects finished
// runs. Adopted-root authority is required both before and after the read.
func (s SQLSigningRenderQualificationReader) ReadSigningRenderQualification(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, recipe v2draft.FrozenRenderRecipe) (SigningRenderQualification, error) {
	var zero SigningRenderQualification
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Adoption == nil || task.Operation != "render.compose" || task.Version != "1" || normalizeCanonicalSigningTask(&task) != nil {
		return zero, ErrInvalid
	}
	roots, err := canonicalTaskOutputRefs(task.Outputs)
	if err != nil || len(roots) != 1 {
		return zero, ErrInvalid
	}
	check := func() error {
		root, err := s.Adoption.ReadSigningAdoptedRenderRoot(ctx, who, ready, task)
		if err != nil {
			return err
		}
		if root != roots[0] {
			return ErrInvalid
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return zero, err
	}
	var result SigningRenderQualification
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var pin, profile []byte
		err := tx.QueryRow(ctx, `SELECT r.v26_qualification_pin,t.execution_profile
   FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
   JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
   WHERE t.tenant_id=$1 AND t.id=$2 AND t.run_id=$3 AND t.canonical_result_id=$4
   AND t.attempt=$5 AND t.fencing_token=$6 AND t.state='succeeded' AND NOT t.cancel_requested
   AND t.operation='render.compose' AND t.operation_version='1' AND w.project_id=$7
   AND r.kind='v2' AND r.graph_sealed AND r.v26_qualification_pin IS NOT NULL`, ready.Tenant, task.Task, task.Run, task.Result, task.Attempt, task.Fence, ready.Project).Scan(&pin, &profile)
		if err != nil {
			return err
		}
		result, err = DecodeSigningRenderQualification(pin, recipe.Scalars.ModelLockSHA, recipe.Scalars.ICCLockSHA, profile)
		return err
	})
	if err != nil {
		return zero, err
	}
	if err := check(); err != nil {
		return zero, err
	}
	return result, nil
}
