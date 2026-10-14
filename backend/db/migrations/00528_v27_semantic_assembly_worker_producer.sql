-- +goose Up
-- Adopted producer evidence under the live CPU assembly lease; exact Params location only.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_assembly_worker_producer(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_params_sha text,p_digest text,p_candidate uuid,p_sha text)
RETURNS TABLE(producer_task_id uuid,producer_attempt integer,producer_fence bigint,producer_result_id uuid,params_artifact_id uuid,params_sha text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT producer.id,producer.attempt,producer.fencing_token,z.id,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes
 FROM photo.tasks assembly
 JOIN photo.v2_workflows w ON w.tenant_id=assembly.tenant_id AND w.run_id=assembly.run_id
 JOIN photo.v2_task_inputs candidate ON candidate.tenant_id=assembly.tenant_id AND candidate.task_id=assembly.id
  AND candidate.project_id=w.project_id AND candidate.artifact_id=p_candidate AND candidate.sha256=p_sha
  AND candidate.contract='TransferCandidate'
 JOIN photo.artifacts output ON output.tenant_id=assembly.tenant_id AND output.id=candidate.artifact_id
 JOIN photo.tasks producer ON producer.tenant_id=assembly.tenant_id AND producer.id=output.created_by_task_id
  AND producer.run_id=assembly.run_id
 JOIN photo.task_results z ON z.tenant_id=producer.tenant_id AND z.id=producer.canonical_result_id
  AND z.task_id=producer.id AND z.attempt=producer.attempt AND z.fencing_token=producer.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=producer.tenant_id AND a.id=producer.params_artifact_id
 WHERE assembly.tenant_id=p_tenant AND assembly.id=p_task
 AND assembly.run_id=p_run
 AND photo.v27_semantic_assembly_worker_qualification(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_params_sha,p_digest) IS NOT NULL
 AND photo.v27_semantic_candidate_scope(p_tenant,w.id,p_candidate,p_sha)
 AND producer.operation='semantic.transfer' AND producer.operation_version='1' AND producer.resource_class='gpu'
 AND producer.state='succeeded' AND NOT producer.cancel_requested
 AND photo.v2_input_closure_ready(producer.id)
 AND CASE WHEN jsonb_typeof(z.outputs)='array' THEN jsonb_array_length(z.outputs)=1 ELSE false END
 AND output.produced_attempt=producer.attempt AND output.produced_fence=producer.fencing_token
 AND output.sha256=p_sha AND output.state='ready' AND output.verified_at IS NOT NULL
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',p_candidate,'sha256',p_sha))
 AND a.bytes BETWEEN 2 AND 1048576
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=producer.tenant_id
  AND i.project_id=w.project_id AND i.task_id=producer.id AND i.artifact_id=a.id
  AND i.sha256=a.sha256 AND i.contract='Params');
$$;
ALTER FUNCTION photo.v27_semantic_assembly_worker_producer(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_assembly_worker_producer(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_assembly_worker_producer(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
