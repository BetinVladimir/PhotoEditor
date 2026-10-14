-- Fit must have declared the exact evidence ref. Evaluate may read its ready
-- event-owned document only under the existing live expansion lease.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v23_evaluate_frozen_root(p_tenant uuid,p_run uuid,
 p_expansion uuid,p_token uuid,p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text,output_role text,
 created_by_task_id uuid,produced_attempt integer,produced_fence bigint)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,
  a.media_type,coalesce(a.output_role,''),a.created_by_task_id,a.produced_attempt,
  a.produced_fence
 FROM photo.graph_expansions e
 JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 JOIN photo.tasks fit ON fit.tenant_id=e.tenant_id
  AND fit.run_id=e.run_id AND fit.id=e.parent_task_id
 JOIN photo.v2_task_inputs i ON i.tenant_id=e.tenant_id
  AND i.task_id=fit.id AND i.artifact_id=p_artifact
  AND i.sha256=p_sha AND i.contract=p_contract
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
  AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_expansion
  AND e.kind='v2.learning.evaluate/1' AND e.state='open'
  AND e.expansion_token=p_token AND e.expansion_lease_until>clock_timestamp()
  AND r.kind='v2' AND r.status='running' AND NOT r.graph_sealed
  AND NOT r.cancel_requested
  AND fit.operation='learning.fit' AND fit.operation_version='1'
  AND fit.state='succeeded' AND fit.canonical_result_id IS NOT NULL
  AND p_contract IN ('WorkingImage','FrozenRecipe','MaskRef',
   'FaceObservations','CandidateProfile','Calibration','ConsentSnapshot',
   'FrozenDefectHoldoutEvidence')
  AND (p_contract<>'FrozenDefectHoldoutEvidence' OR EXISTS(
   SELECT 1 FROM photo.backend_documents d
   WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND
    d.artifact_id=a.id AND d.sha256=a.sha256 AND
    d.purpose='v23_defect_holdout' AND d.state='ready' AND
    d.object_version=a.object_version AND d.bucket=a.bucket AND
    d.bytes=a.bytes AND a.output_role='v23_defect_holdout' AND
    a.media_type='application/json' AND a.object_key=
     'tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||a.id))
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_frozen_root(uuid,uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_frozen_root(uuid,uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_frozen_root(uuid,uuid,uuid,uuid,uuid,text,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
