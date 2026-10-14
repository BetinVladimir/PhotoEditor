-- +goose Up
-- Source-authorized canonical person producer Params/report object discovery.
-- Byte contracts and scientific qualification are independent checks.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_person_evidence_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_version uuid,p_source uuid,p_source_sha text,p_root uuid,p_root_sha text,p_child uuid,p_child_sha text)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,backend text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH producer AS (
 SELECT task.id,task.params_artifact_id,task.execution_profile->>'backend' AS backend,task.attempt,task.fencing_token,result.outputs
 FROM photo.artifacts root JOIN photo.tasks task ON task.tenant_id=root.tenant_id AND task.id=root.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.id=task.canonical_result_id AND result.task_id=task.id
 WHERE root.tenant_id=p_tenant AND root.id=p_root AND root.sha256=p_root_sha
 AND task.execution_profile->>'backend' IN('cpu','cuda','mps')
 AND photo.semantic_proposal_person_mask_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_root,p_root_sha,p_child,p_child_sha)
 ), evidence AS (
 SELECT 'PersonParams'::text AS contract,a.*,p.backend FROM producer p JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=p.params_artifact_id
 UNION ALL
 SELECT 'PersonReport'::text,a.*,p.backend FROM producer p JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.created_by_task_id=p.id
 AND a.output_role='person_report' AND a.produced_attempt=p.attempt AND a.produced_fence=p.fencing_token
 AND p.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 ), ready AS (
 SELECT * FROM evidence WHERE state='ready' AND verified_at IS NOT NULL AND media_type='application/json'
 AND object_version IS NOT NULL AND object_version NOT IN('','null') AND bytes BETWEEN 2 AND 1048576
 AND (expires_at IS NULL OR expires_at>clock_timestamp())
 )
 SELECT contract,id,sha256,bucket,object_key,object_version,bytes,backend FROM ready
 WHERE (SELECT count(*) FROM ready)=2 AND (SELECT count(*) FROM ready WHERE contract='PersonReport')=1
 AND (SELECT count(*) FROM ready WHERE contract='PersonParams')=1
 ORDER BY contract;
$$;
ALTER FUNCTION photo.semantic_proposal_person_evidence_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_person_evidence_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_person_evidence_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
