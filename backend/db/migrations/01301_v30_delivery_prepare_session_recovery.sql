-- +goose Up
-- Reauthorize identical pre-activation delivery evidence after session rotation.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.save_v30_delivery_prepare_proof(p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,
 p_run uuid,p_task uuid,p_params uuid,p_sha text,p_json text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;d photo.backend_documents%ROWTYPE;params jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_project IS NULL OR p_run IS NULL OR p_task IS NULL OR p_params IS NULL
 OR p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 65536
 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha
 OR p_proof IS NULL OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object' OR pg_column_size(p_proof)>16777216
 OR p_proof->>'project_id' IS DISTINCT FROM p_project::text OR p_proof->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 params:=p_json::jsonb;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT count(*) FROM jsonb_object_keys(params))<>2 OR jsonb_typeof(params->'input') IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(params->'input'))<>2 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task
 AND purpose='task_params' AND artifact_id=p_params AND sha256=p_sha AND bytes=octet_length(p_json) AND state='ready' FOR UPDATE;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.created_by IS DISTINCT FROM p_actor OR r.cancel_requested
 OR t.id IS NULL OR t.operation IS DISTINCT FROM 'v2.delivery.prepare' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.params_artifact_id IS DISTINCT FROM p_params OR t.cancel_requested
 OR d.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.project_id=p_project) THEN RETURN false;END IF;
 IF d.v30_delivery_proof IS NOT NULL THEN
  IF d.v30_delivery_proof=p_proof THEN
   RETURN photo.v30_delivery_prepare_source_ready(p_tenant,p_run,p_task,p_params,p_sha,p_json);
  END IF;
  -- The same actor may resume a pre-activation intent after session rotation.
  -- Only the authorization witness changes; immutable compilation bytes must
  -- remain identical. Never rewrite proof for running/sealed/attempted work.
  IF r.status IS DISTINCT FROM 'draft' OR r.graph_sealed
  OR t.state IS DISTINCT FROM 'blocked' OR t.attempt<>0
  OR (d.v30_delivery_proof-'auth_session_id') IS DISTINCT FROM (p_proof-'auth_session_id')
  THEN RETURN false;END IF;
 END IF;
 IF r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR t.state IS DISTINCT FROM 'blocked' OR t.attempt<>0 THEN RETURN false;END IF;
 UPDATE photo.backend_documents SET v30_delivery_proof=p_proof WHERE id=d.id;
 IF photo.v30_delivery_prepare_source_ready(p_tenant,p_run,p_task,p_params,p_sha,p_json) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'Delivery prepare proof refused' USING ERRCODE='23514';
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v30_delivery_prepare_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v30_delivery_prepare_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.save_v30_delivery_prepare_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- Forward-only: intentionally retain current authorization recovery semantics.
