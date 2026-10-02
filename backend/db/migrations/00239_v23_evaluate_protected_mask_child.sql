-- A protected mask array must be a canonical task output of the same project.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_protected_mask_child(p_tenant uuid,p_run uuid,
 p_expansion uuid,p_token uuid,p_parent uuid,p_parent_sha text,
 p_child uuid,p_child_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text,output_role text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT child.id,child.sha256,child.bytes,child.bucket,child.object_key,
  child.object_version,child.media_type,child.output_role
 FROM photo.graph_expansions e
 JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.tasks fit ON fit.tenant_id=e.tenant_id
  AND fit.run_id=e.run_id AND fit.id=e.parent_task_id
 JOIN photo.v2_task_inputs i ON i.tenant_id=e.tenant_id
  AND i.task_id=fit.id AND i.artifact_id=p_parent
  AND i.sha256=p_parent_sha AND i.contract='MaskRef'
 JOIN photo.artifacts parent ON parent.tenant_id=i.tenant_id
  AND parent.id=i.artifact_id AND parent.sha256=i.sha256
 JOIN photo.artifacts child ON child.tenant_id=parent.tenant_id
  AND child.id=p_child AND child.sha256=p_child_sha
 JOIN photo.tasks producer ON producer.tenant_id=child.tenant_id
  AND producer.id=child.created_by_task_id
 JOIN photo.v2_workflows producer_w ON producer_w.tenant_id=producer.tenant_id
  AND producer_w.run_id=producer.run_id AND producer_w.project_id=w.project_id
 WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_expansion
  AND e.kind='v2.learning.evaluate/1' AND e.state='open'
  AND e.expansion_token=p_token AND e.expansion_lease_until>clock_timestamp()
  AND r.kind='v2' AND r.status='running' AND NOT r.graph_sealed
  AND NOT r.cancel_requested AND w.workflow_type='learning'
  AND fit.operation='learning.fit' AND fit.operation_version='1'
  AND fit.state='succeeded' AND fit.canonical_result_id IS NOT NULL
  AND parent.state='ready' AND parent.media_type='application/json'
  AND parent.verified_at IS NOT NULL
  AND (parent.expires_at IS NULL OR parent.expires_at>clock_timestamp())
  AND child.state='ready' AND child.verified_at IS NOT NULL
  AND child.media_type='application/octet-stream' AND child.bytes>0
  AND child.output_role IN ('mask_protected','defect_protection',
   'transformed_mask')
  AND child.object_version IS NOT NULL
  AND child.object_version NOT IN ('','null')
  AND child.produced_attempt=producer.attempt
  AND child.produced_fence=producer.fencing_token
  AND producer.state='succeeded' AND producer.canonical_result_id IS NOT NULL
  AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_protected_mask_child(uuid,uuid,uuid,uuid,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_protected_mask_child(uuid,uuid,uuid,uuid,uuid,text,uuid,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_protected_mask_child(uuid,uuid,uuid,uuid,uuid,text,uuid,text)
 TO photo_scheduler;
