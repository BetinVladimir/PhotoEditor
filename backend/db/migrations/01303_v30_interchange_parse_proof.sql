-- +goose Up
-- API admission installs only authenticated metadata, never archive declarations.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v30_interchange_parse_proof(p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,
 p_run uuid,p_task uuid,p_params uuid,p_sha text,p_json text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;d photo.backend_documents%ROWTYPE;
 params jsonb;request jsonb;bundle jsonb;proof jsonb;expanded bigint;files integer;archive_bytes bigint;estimate jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_project IS NULL
 OR p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 65536
 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
 OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 params:=p_json::jsonb;request:=params->'request';bundle:=request->'bundle';
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT count(*) FROM jsonb_object_keys(params))<>4
 OR NOT(params ?& ARRAY['schema_version','request','max_expanded_bytes','max_files'])
 OR jsonb_typeof(request) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(request))<>4
 OR NOT(request ?& ARRAY['project_id','direction','format','bundle'])
 OR request->>'project_id' IS DISTINCT FROM p_project::text OR request->>'direction' IS DISTINCT FROM 'import'
 OR request->>'format' IS DISTINCT FROM 'photoeditor_bundle'
 OR jsonb_typeof(bundle) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(bundle))<>2
 OR coalesce(bundle->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR bundle->>'artifact_id'='00000000-0000-0000-0000-000000000000'
 OR coalesce(bundle->>'sha256','') !~ '^[0-9a-f]{64}$'
 OR coalesce(params->>'max_expanded_bytes','') !~ '^[1-9][0-9]*$'
 OR coalesce(params->>'max_files','') !~ '^[1-9][0-9]*$' THEN RETURN false;END IF;
 expanded:=(params->>'max_expanded_bytes')::bigint;files:=(params->>'max_files')::integer;
 IF expanded NOT BETWEEN 1 AND 137438953472 OR files NOT BETWEEN 1 AND 30000 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND project_id=p_project AND workflow_type='interchange' FOR SHARE;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task
 AND artifact_id=p_params AND sha256=p_sha AND purpose='task_params' AND state='ready' AND bytes=octet_length(p_json) FOR UPDATE;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.created_by IS DISTINCT FROM p_actor OR r.cancel_requested
 OR t.id IS NULL OR t.operation IS DISTINCT FROM 'v2.interchange.parse' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.params_artifact_id IS DISTINCT FROM p_params OR t.cancel_requested
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR w.id IS NULL OR d.id IS NULL OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InterchangeArchive')<>1
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InterchangeArchive'
 AND artifact_id=(bundle->>'artifact_id')::uuid AND sha256=bundle->>'sha256')
 OR NOT EXISTS(SELECT 1 FROM photo.snapshots sn JOIN photo.snapshot_items si ON si.tenant_id=sn.tenant_id AND si.snapshot_id=sn.id
 WHERE sn.tenant_id=p_tenant AND sn.id=w.input_snapshot_id AND sn.kind='v2' AND sn.expires_at IS NULL AND sn.item_count=1
 AND si.ordinal=0 AND si.asset_version_id IS NULL AND si.artifact_id=(bundle->>'artifact_id')::uuid AND si.sha256=bundle->>'sha256') THEN RETURN false;END IF;
 SELECT bytes INTO archive_bytes FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(bundle->>'artifact_id')::uuid
 AND sha256=bundle->>'sha256' AND state='ready' AND verified_at IS NOT NULL AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 estimate:=t.execution_profile->'resource_estimate';
 IF archive_bytes IS NULL OR archive_bytes NOT BETWEEN 1 AND 68719476736
 OR coalesce((estimate->>'output_bytes')::numeric,0)<expanded+8388608
 OR coalesce((estimate->>'scratch_bytes')::numeric,0)<archive_bytes+expanded+8388608
 OR coalesce((estimate->>'ram_bytes')::numeric,0)<536870912
 OR estimate->'vram_bytes' IS DISTINCT FROM '0'::jsonb THEN RETURN false;END IF;
 proof:=jsonb_build_object('schema_version',1,'auth_session_id',p_session,'project_id',p_project,'bundle',bundle);
 IF d.v30_interchange_proof=proof THEN RETURN true;END IF;
 IF r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR t.state IS DISTINCT FROM 'blocked' OR t.attempt<>0
 OR (d.v30_interchange_proof IS NOT NULL AND d.v30_interchange_proof-'auth_session_id' IS DISTINCT FROM proof-'auth_session_id') THEN RETURN false;END IF;
 UPDATE photo.backend_documents SET v30_interchange_proof=proof WHERE id=d.id;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v30_interchange_parse_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v30_interchange_parse_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.save_v30_interchange_parse_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
