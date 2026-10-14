package provenance

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// CanonicalSigningTask is execution evidence, not an adopted generation/review
// claim. Operation-specific resolvers must establish those claims from their
// original Params and adopted documents before constructing SigningAction.
type CanonicalSigningTask struct {
	Task, Run, Result                             uuid.UUID
	Attempt                                       int
	Fence                                         int64
	Operation, Version, InputDigest, ResultDigest string
	Params                                        v2input.Ref
	Inputs, Outputs, Metrics                      json.RawMessage
	ReviewRequired                                bool
}

// SQLSigningTaskCatalog exposes export ancestors. Cross-run transitions require
// an exact input ref adopted as a live same-project asset-version master with
// matching created_by_run_id and canonical fenced producer evidence.
type SQLSigningTaskCatalog struct {
	DB      *pgxpool.Pool
	Exports SigningExportCatalog
}

func (s SQLSigningTaskCatalog) ReadCanonicalSigningTask(ctx context.Context, who auth.Identity, ready ReadySigningExport, id uuid.UUID) (CanonicalSigningTask, error) {
	var result CanonicalSigningTask
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.DB == nil || s.Exports == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || ready.Task == uuid.Nil || ready.Run == uuid.Nil || id == uuid.Nil {
		return result, ErrInvalid
	}
	check := func() error {
		current, err := s.Exports.LookupSigningExport(ctx, who, ready.Tenant, ready.ExportID)
		if err != nil {
			return err
		}
		if current != ready {
			return ErrInvalid
		}
		return nil
	}
	if err := check(); err != nil {
		return result, err
	}
	err := tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		// UNION deduplicates repeated ancestors and terminates even for corrupt
		// cycles. No result is returned if the ancestry exceeds the contract limit.
		var inputs, outputs, metrics []byte
		err := tx.QueryRow(ctx, signingTaskEvidenceSQL, ready.Tenant, ready.Task, ready.Run, id, ready.Project).Scan(
			&result.Task, &result.Run, &result.Result, &result.Attempt, &result.Fence, &result.Operation, &result.Version,
			&result.InputDigest, &result.ResultDigest, &result.Params.ArtifactID, &result.Params.SHA256,
			&inputs, &outputs, &metrics, &result.ReviewRequired)
		if err != nil {
			return err
		}
		result.Inputs, result.Outputs, result.Metrics = inputs, outputs, metrics
		return normalizeCanonicalSigningTask(&result)
	})
	if err != nil {
		return CanonicalSigningTask{}, err
	}
	if err := check(); err != nil {
		return CanonicalSigningTask{}, err
	}
	if err := ctx.Err(); err != nil {
		return CanonicalSigningTask{}, err
	}
	return result, nil
}

func normalizeCanonicalSigningTask(value *CanonicalSigningTask) error {
	if value == nil || value.Task == uuid.Nil || value.Run == uuid.Nil || value.Result == uuid.Nil || value.Attempt < 1 || value.Fence < 1 || !text(value.Operation, 200) || !text(value.Version, 100) || !digest(value.InputDigest) || !digest(value.ResultDigest) || !ref(value.Params) {
		return ErrInvalid
	}
	// PostgreSQL jsonb whitespace/order is not signed wire encoding. Normalize
	// only these DB columns; original artifact bytes still require exact SHA/JCS.
	for _, field := range []struct {
		raw   *json.RawMessage
		limit int
		kind  byte
	}{
		{&value.Inputs, 16 << 20, '['}, {&value.Outputs, 16 << 20, '['}, {&value.Metrics, 8 << 20, '{'},
	} {
		if len(*field.raw) < 2 || len(*field.raw) > field.limit {
			return ErrInvalid
		}
		encoded, err := canonical.Bytes(*field.raw)
		if err != nil || len(encoded) < 2 || encoded[0] != field.kind {
			return ErrInvalid
		}
		*field.raw = append(json.RawMessage(nil), encoded...)
	}
	return nil
}

// Parameters: tenant, final task, final run, requested task, authorized project.
const signingTaskEvidenceSQL = `WITH RECURSIVE ancestors(id) AS (
   SELECT t.id FROM photo.tasks t WHERE t.tenant_id=$1 AND t.id=$2 AND t.run_id=$3
   UNION
   SELECT next_task.id FROM ancestors ancestry
   JOIN photo.tasks child ON child.tenant_id=$1 AND child.id=ancestry.id AND child.state='succeeded'
   CROSS JOIN LATERAL (
    SELECT edge.prerequisite_id AS id FROM photo.task_edges edge
    JOIN photo.tasks parent ON parent.tenant_id=edge.tenant_id AND parent.id=edge.prerequisite_id AND parent.run_id=child.run_id
    WHERE edge.tenant_id=$1 AND edge.run_id=child.run_id AND edge.dependent_id=child.id
    UNION
    SELECT producer.id FROM photo.artifacts input_artifact
    JOIN photo.asset_versions version ON version.tenant_id=input_artifact.tenant_id AND version.master_artifact_id=input_artifact.id
    JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
    JOIN photo.tasks producer ON producer.tenant_id=input_artifact.tenant_id AND producer.id=input_artifact.created_by_task_id
    JOIN photo.task_results proof ON proof.tenant_id=producer.tenant_id AND proof.task_id=producer.id AND proof.id=producer.canonical_result_id
    WHERE input_artifact.tenant_id=$1 AND asset.project_id=$5 AND asset.deleted_at IS NULL
     AND version.created_by_run_id=producer.run_id AND producer.run_id<>child.run_id
     AND producer.state='succeeded' AND proof.attempt=producer.attempt AND proof.fencing_token=producer.fencing_token
     AND input_artifact.state='ready' AND input_artifact.verified_at IS NOT NULL
     AND input_artifact.object_version NOT IN ('','null')
     AND input_artifact.media_type='application/json' AND input_artifact.bytes BETWEEN 2 AND 1048576
     AND (input_artifact.expires_at IS NULL OR input_artifact.expires_at>clock_timestamp())
     AND input_artifact.produced_attempt=producer.attempt AND input_artifact.produced_fence=producer.fencing_token
     AND ` + signingCrossRunInputPredicateSQL + `
     AND ` + signingCrossRunProducerOutputPredicateSQL + `
   ) next_task
  ), bounded AS (SELECT id FROM ancestors LIMIT 5001)
  SELECT t.id,t.run_id,z.id,t.attempt,t.fencing_token,t.operation,t.operation_version,
   t.input_digest,z.result_digest,p.id,p.sha256,t.inputs,z.outputs,z.metrics,z.review_required
  FROM photo.tasks t
  JOIN bounded a ON a.id=t.id
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
  JOIN photo.artifacts p ON p.tenant_id=t.tenant_id AND p.id=t.params_artifact_id
  WHERE t.tenant_id=$1 AND t.id=$4 AND (SELECT count(*) FROM bounded)<=5000
   AND t.state='succeeded' AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
   AND p.state='ready' AND p.verified_at IS NOT NULL AND p.object_version NOT IN ('','null')
   AND p.media_type='application/json' AND p.bytes BETWEEN 2 AND 8388608
   AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())
   AND octet_length(t.inputs::text)<=16777216 AND octet_length(z.outputs::text)<=16777216
   AND octet_length(z.metrics::text)<=8388608
  FOR SHARE OF t,z,p`

// Render and negative transports keep content pins in the admitted typed inventory.
// This predicate grants ancestry discovery only; physical closure and operation
// claim resolvers still independently prove every byte and producer boundary.
const signingCrossRunInputPredicateSQL = `(child.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',input_artifact.id::text,'sha256',input_artifact.sha256))
 OR (child.operation IN ('render.compose','optical.negative') AND child.operation_version='1' AND EXISTS(
 SELECT 1 FROM photo.v2_task_inputs registered
 JOIN photo.v2_workflows consuming ON consuming.tenant_id=registered.tenant_id
  AND consuming.project_id=registered.project_id AND consuming.run_id=child.run_id
 WHERE registered.tenant_id=child.tenant_id AND registered.task_id=child.id
  AND registered.project_id=$5 AND registered.artifact_id=input_artifact.id
  AND registered.sha256=input_artifact.sha256 AND registered.contract='WorkingImage')))`

// A render or negative WorkingImage is an adopted ImageResult child, not a declared task
// output. An accepted asset-version boundary plus exact root/child links makes
// the producer discoverable; the claims resolver must still read root bytes.
const signingCrossRunProducerOutputPredicateSQL = `(proof.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',input_artifact.id::text,'sha256',input_artifact.sha256))
 OR (producer.operation IN ('render.compose','optical.negative') AND producer.operation_version='1'
  AND NOT producer.cancel_requested AND version.review_state='accepted'
  AND ` + signingCrossRunWorkingImageRoleSQL + ` AND EXISTS(
 SELECT 1 FROM photo.v2_workflows render_workflow
 JOIN photo.v2_artifact_links child_link ON child_link.tenant_id=render_workflow.tenant_id
  AND child_link.project_id=render_workflow.project_id AND child_link.workflow_id=render_workflow.id
 JOIN photo.v2_artifact_links root_link ON root_link.tenant_id=render_workflow.tenant_id
  AND root_link.project_id=render_workflow.project_id AND root_link.workflow_id=render_workflow.id
 JOIN photo.artifacts root_artifact ON root_artifact.tenant_id=root_link.tenant_id
  AND root_artifact.id=root_link.artifact_id AND root_artifact.sha256=root_link.sha256
 WHERE render_workflow.tenant_id=producer.tenant_id AND render_workflow.run_id=producer.run_id
  AND render_workflow.project_id=$5 AND render_workflow.renderer_version='v2.2'
  AND child_link.artifact_id=input_artifact.id AND child_link.sha256=input_artifact.sha256
  AND child_link.purpose=encode(sha256(convert_to('tasks/'||producer.id||'/$/working_image','UTF8')),'hex')
  AND root_link.ordinal=0 AND root_link.purpose=encode(sha256(convert_to('tasks/'||producer.id||'/$','UTF8')),'hex')
  AND proof.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root_artifact.id::text,'sha256',root_artifact.sha256))
  AND root_artifact.created_by_task_id=producer.id AND root_artifact.produced_attempt=producer.attempt
  AND root_artifact.produced_fence=producer.fencing_token AND root_artifact.output_role='document'
  AND root_artifact.media_type='application/json' AND root_artifact.bytes BETWEEN 2 AND 1048576
  AND root_artifact.state='ready' AND root_artifact.verified_at IS NOT NULL
  AND root_artifact.object_version NOT IN('','null')
  AND (root_artifact.expires_at IS NULL OR root_artifact.expires_at>clock_timestamp()))))`

// Preserve operation-specific adoption roles; accepting either role for both
// operations would admit artifacts outside their respective adopted contracts.
const signingCrossRunWorkingImageRoleSQL = `((producer.operation='render.compose' AND input_artifact.output_role='document')
 OR (producer.operation='optical.negative' AND input_artifact.output_role='working_image'))`
