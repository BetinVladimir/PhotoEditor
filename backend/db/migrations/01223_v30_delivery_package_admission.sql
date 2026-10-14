-- +goose Up
-- Attach metadata only to an existing, proof-checked blocked preparation task.
-- This writer does not grant permission to dispatch or publish a delivery.
-- +goose StatementBegin
CREATE FUNCTION photo.admit_v30_delivery_package(p_tenant uuid,p_actor uuid,p_session uuid,
 p_package uuid,p_workflow uuid,p_task uuid,p_name text,p_policy uuid,p_policy_sha text,
 p_policy_json text,p_params_sha text,p_params_json text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 d photo.delivery_packages%ROWTYPE;proof jsonb;policy jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_package IS NULL OR p_package='00000000-0000-0000-0000-000000000000'::uuid
 OR p_workflow IS NULL OR p_task IS NULL OR p_policy IS NULL OR p_name IS NULL OR length(p_name) NOT BETWEEN 1 AND 200
 OR p_policy_json IS NULL OR octet_length(p_policy_json) NOT BETWEEN 2 AND 1048576
 OR p_params_json IS NULL OR octet_length(p_params_json) NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(p_policy_json,'UTF8')),'hex') IS DISTINCT FROM p_policy_sha
 OR encode(sha256(convert_to(p_params_json,'UTF8')),'hex') IS DISTINCT FROM p_params_sha THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow FOR SHARE;
 IF NOT FOUND OR w.workflow_type<>'delivery' OR w.renderer_version<>'v2.2'
 OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=w.run_id AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.created_by IS DISTINCT FROM p_actor OR r.cancel_requested
 OR r.status<>'draft' OR r.graph_sealed OR r.input_snapshot_id IS DISTINCT FROM w.input_snapshot_id
 OR t.id IS NULL OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested
 OR t.operation<>'v2.delivery.prepare' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=r.id) THEN RETURN false;END IF;
 SELECT b.v30_delivery_proof INTO proof FROM photo.backend_documents b
 WHERE b.tenant_id=p_tenant AND b.run_id=r.id AND b.owner_id=t.id AND b.purpose='task_params'
 AND b.artifact_id=t.params_artifact_id AND b.sha256=p_params_sha AND b.bytes=octet_length(p_params_json)
 AND b.state='ready' FOR SHARE;
 IF proof IS NULL OR proof->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR photo.v30_delivery_prepare_source_ready(p_tenant,r.id,t.id,t.params_artifact_id,p_params_sha,p_params_json) IS DISTINCT FROM true THEN RETURN false;END IF;
 policy:=p_policy_json::jsonb;
 IF policy IS DISTINCT FROM (proof->>'frozen_input_jcs')::jsonb->'policy' THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_policy AND sha256=p_policy_sha
 AND media_type='application/json' AND bytes=octet_length(p_policy_json) AND state='ready'
 AND verified_at IS NOT NULL AND object_version IS NOT NULL AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=p_tenant AND workflow_id=p_workflow FOR UPDATE;
 IF FOUND THEN
  RETURN d.id=p_package AND d.project_id=w.project_id AND d.snapshot_id=w.input_snapshot_id
   AND d.name=p_name AND d.policy_artifact_id=p_policy AND d.state='building'
   AND d.manifest_artifact_id IS NULL AND d.published_at IS NULL;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.delivery_packages WHERE id=p_package) THEN RETURN false;END IF;
 INSERT INTO photo.delivery_packages(id,tenant_id,project_id,workflow_id,snapshot_id,name,policy_artifact_id,state)
 VALUES(p_package,p_tenant,w.project_id,p_workflow,w.input_snapshot_id,p_name,p_policy,'building');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.admit_v30_delivery_package(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_v30_delivery_package(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.admit_v30_delivery_package(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
