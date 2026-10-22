-- +goose Up
-- Exact accepted Region recovery is independent of fresh runtime availability.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_accepted_recovery(p_tenant uuid,p_asset uuid,p_actor uuid,p_session uuid,p_key text,p_sha text,p_run uuid,p_workflow uuid,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt photo.idempotency_records%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;t photo.tasks%ROWTYPE;n jsonb;params jsonb;source_row record;recipe_row record;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_key IS NULL OR p_key COLLATE "C" !~ '^[!-~]{1,128}$' OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_actor IS NULL OR p_session IS NULL THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO receipt FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=p_actor AND i.route='V2.Admit' AND i.key=p_key;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF receipt.request_hash IS DISTINCT FROM p_sha THEN RAISE EXCEPTION 'REGION_RECOVERY_CONFLICT' USING ERRCODE='23505';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=receipt.resource_id;
 -- An incomplete draft remains owned by the ordinary preparation/resume path.
 IF receipt.response_status IS NULL AND (r.id IS NULL OR NOT r.graph_sealed) THEN RETURN NULL;END IF;
 IF receipt.response_status IS DISTINCT FROM 202 OR receipt.resource_id IS DISTINCT FROM p_run OR r.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.created_by<>p_actor THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF w.id IS DISTINCT FROM p_workflow OR w.workflow_type<>'preview' OR w.renderer_version<>'v2.2'
  OR r.config_snapshot->>'project_id' IS DISTINCT FROM w.project_id::text
  OR r.config_snapshot->>'input_snapshot_id' IS DISTINCT FROM r.input_snapshot_id::text
  OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
  OR NOT photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';END IF;
 IF jsonb_typeof(r.config_snapshot#>'{v26_region,nodes}') IS DISTINCT FROM 'array' OR jsonb_array_length(r.config_snapshot#>'{v26_region,nodes}')<>1 THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';END IF;
 n:=r.config_snapshot#>'{v26_region,nodes,0}';params:=(n->>'params_jcs')::jsonb;
 IF params->>'tenant_id' IS DISTINCT FROM p_tenant::text OR params->>'asset_id' IS DISTINCT FROM p_asset::text OR params->>'project_id' IS DISTINCT FROM w.project_id::text
  OR (r.config_snapshot#>>'{v26_region,auth_session_id}')::uuid IS NULL OR (r.config_snapshot#>>'{v26_region,auth_session_id}')::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task;
 IF t.id IS NULL OR n->>'task_id' IS DISTINCT FROM p_task::text OR t.operation<>'region.preview' OR t.operation_version<>'1' OR t.resource_class<>'gpu'
  OR t.params_artifact_id::text IS DISTINCT FROM n#>>'{params,artifact_id}' OR t.input_digest IS DISTINCT FROM n->>'input_digest'
  OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM n->'profile'
  OR NOT photo.v2_input_closure_ready(t.id) OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1 THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';END IF;
 IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.sha256=n#>>'{params,sha256}' AND a.sha256=encode(sha256(convert_to(n->>'params_jcs','UTF8')),'hex')) THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';END IF;
 IF EXISTS(SELECT FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR a.expires_at<=clock_timestamp())) THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';END IF;
 SELECT * INTO source_row FROM photo.v26_region_source(p_tenant,p_asset,(params#>>'{request,version_id}')::uuid,p_actor,p_session);
 SELECT * INTO recipe_row FROM photo.v26_region_recipe(p_tenant,p_asset,(params#>>'{request,version_id}')::uuid,p_actor,p_session,(params#>>'{request,recipe,artifact_id}')::uuid,params#>>'{request,recipe,sha256}');
 IF source_row.artifact_id IS NULL OR recipe_row.artifact_id IS NULL OR source_row.project_id IS DISTINCT FROM w.project_id OR recipe_row.project_id IS DISTINCT FROM w.project_id
  OR source_row.artifact_id::text IS DISTINCT FROM params#>>'{source,ref,artifact_id}' OR source_row.sha256 IS DISTINCT FROM params#>>'{source,ref,sha256}'
  OR source_row.bucket IS DISTINCT FROM params#>>'{source,bucket}' OR source_row.object_key IS DISTINCT FROM params#>>'{source,object_key}'
  OR source_row.object_version IS DISTINCT FROM params#>>'{source,object_version}' OR source_row.bytes IS DISTINCT FROM (params#>>'{source,bytes}')::bigint
  OR recipe_row.artifact_id::text IS DISTINCT FROM params#>>'{recipe,ref,artifact_id}' OR recipe_row.sha256 IS DISTINCT FROM params#>>'{recipe,ref,sha256}'
  OR recipe_row.bucket IS DISTINCT FROM params#>>'{recipe,bucket}' OR recipe_row.object_key IS DISTINCT FROM params#>>'{recipe,object_key}'
  OR recipe_row.object_version IS DISTINCT FROM params#>>'{recipe,object_version}' OR recipe_row.bytes IS DISTINCT FROM (params#>>'{recipe,bytes}')::bigint
  OR receipt.response_body IS DISTINCT FROM jsonb_build_object('run_id',p_run,'workflow_id',p_workflow,'state','queued') THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';END IF;
 IF NOT photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) THEN RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';END IF;
 RETURN jsonb_build_object('ticket',receipt.response_body,'config',r.config_snapshot,'config_hash',r.config_hash);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 RAISE EXCEPTION 'REGION_RECOVERY_INVALID' USING ERRCODE='23514';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_region_accepted_recovery(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_accepted_recovery(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_accepted_recovery(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
