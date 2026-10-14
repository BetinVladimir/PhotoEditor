-- +goose Up
-- Launch acknowledgement only: it is never a quality PASS or approval.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_quality_receipt(p_tenant uuid,p_run uuid,p_task uuid,
 p_result uuid,p_root_sha text)
RETURNS TABLE(state text,quality_task uuid)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.status,a.id FROM photo.runs r
 CROSS JOIN LATERAL photo.v29_perspective_preview_source(p_tenant,p_task) s
 JOIN photo.tasks a ON a.tenant_id=r.tenant_id AND a.run_id=r.id AND a.node_key='quality.check:master'
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
  AND r.tenant_id=p_tenant AND r.id=p_run AND r.kind='preview' AND r.graph_sealed
  AND r.v29_perspective_quality_task_id=p_task AND s.result_id=p_result AND s.root_sha=p_root_sha
  AND r.status IN('running','completed','completed_with_errors','failed','cancelled')
  AND r.config_snapshot->>'source_result_id'=s.result_id::text
  AND r.config_snapshot->>'source_attempt'=s.attempt::text AND r.config_snapshot->>'source_fence'=s.fence::text
  AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
  AND r.config_snapshot->>'quality_policy'='master_v1'
  AND a.operation='quality.check' AND a.operation_version='1' AND a.resource_class='cpu'
  AND (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run)=1
  AND (SELECT count(*) FROM photo.backend_documents d JOIN photo.tasks t
   ON t.tenant_id=d.tenant_id AND t.id=d.owner_id AND t.run_id=d.run_id AND t.params_artifact_id=d.artifact_id
   WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.purpose='task_params'
    AND d.state='ready' AND d.v29_perspective_quality_params IS NOT NULL)=1;
$$;
ALTER FUNCTION photo.v29_perspective_quality_receipt(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_quality_receipt(uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_quality_receipt(uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
