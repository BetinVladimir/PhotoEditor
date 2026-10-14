-- +goose Up
-- Read observation only. Success adoption must recheck under locks after object IO.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_quality_result(p_tenant uuid,p_run uuid)
RETURNS TABLE(task_id uuid,result_id uuid,attempt integer,fence bigint,params text,
 output_role text,artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH candidate AS (
 SELECT t.id AS task_id,z.id AS result_id,z.attempt,z.fencing_token,d.v29_multi_quality_params AS params,z.outputs
 FROM photo.runs r
 CROSS JOIN LATERAL photo.v29_multi_preview_source(r.tenant_id,r.v29_multi_quality_task_id,r.config_snapshot->>'operation') s
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.node_key='quality.check:master'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id
  AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 JOIN photo.artifacts pa ON pa.tenant_id=d.tenant_id AND pa.id=d.artifact_id AND pa.sha256=d.sha256
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid AND r.tenant_id=p_tenant AND r.id=p_run AND r.kind='preview' AND r.status='running'
 AND r.config_snapshot->>'stage'='master' AND r.config_snapshot->>'threshold_profile_id'='master_v1'
 AND r.v29_multi_quality_task_id IS NOT NULL AND r.graph_sealed AND NOT r.cancel_requested
 AND r.config_snapshot->>'source_result_id'=s.result_id::text
 AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
 AND r.config_snapshot->>'source_attempt'=s.attempt::text AND r.config_snapshot->>'source_fence'=s.fence::text
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.operation='quality.check' AND t.operation_version='1'
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token AND jsonb_array_length(z.outputs)=1
 AND d.v29_multi_quality_params IS NOT NULL AND d.v29_multi_quality_params::jsonb->>'threshold_profile_id'='master_v1'
 AND pa.state='ready' AND pa.verified_at IS NOT NULL AND pa.bytes=d.bytes AND pa.bucket=d.bucket
 AND pa.object_version=d.object_version AND pa.object_version NOT IN('','null')
 AND (pa.expires_at IS NULL OR pa.expires_at>clock_timestamp())
 ), valid_outputs AS (
 SELECT c.task_id,c.result_id,c.attempt,c.fencing_token,c.params,a.output_role,a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM candidate c JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.created_by_task_id=c.task_id
 AND a.produced_attempt=c.attempt AND a.produced_fence=c.fencing_token
 JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.task_id=c.task_id AND u.artifact_id=a.id
 AND u.attempt=c.attempt AND u.fence=c.fencing_token AND u.state='ready' AND u.role=a.output_role
 AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version
 AND u.sha256=a.sha256 AND u.bytes=a.bytes AND u.media_type=a.media_type
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.bytes>0
 AND c.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND a.output_role='quality_report' AND a.media_type='application/json' AND a.bytes<=1048576
 )
 SELECT v.* FROM valid_outputs v WHERE (SELECT count(*) FROM valid_outputs)=1
 AND (SELECT count(DISTINCT output_role) FROM valid_outputs)=1 ORDER BY v.output_role;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v29_multi_quality_result(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_quality_result(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_multi_quality_result(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
