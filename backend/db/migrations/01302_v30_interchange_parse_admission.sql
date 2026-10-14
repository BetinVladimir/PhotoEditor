-- +goose Up
-- Proof is populated only by the authorized API creation coordinator (not installed here).
ALTER TABLE photo.backend_documents ADD COLUMN v30_interchange_proof jsonb;
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_parse_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;s photo.resource_slots%ROWTYPE;
 params jsonb;root jsonb;root_contract text;root_key text;proof jsonb;w photo.v2_workflows%ROWTYPE;archive_bytes bigint;expanded bigint;files integer;estimate jsonb;
BEGIN
 IF p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 65536
  OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_digest IS NULL OR p_digest !~ '^[0-9a-f]{64}$'
  OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.worker_registrations WHERE tenant_id=p_tenant
  AND worker_id=p_worker AND db_principal=session_user AND enabled) THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.graph_sealed IS DISTINCT FROM true
  OR r.cancel_requested IS DISTINCT FROM false OR r.status NOT IN('queued','running')
  OR t.id IS NULL OR t.operation IS DISTINCT FROM 'v2.interchange.parse'
  OR t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
  OR t.state IS DISTINCT FROM 'running' OR t.cancel_requested IS DISTINCT FROM false
  OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
  OR t.worker_id IS DISTINCT FROM p_worker OR t.params_artifact_id IS DISTINCT FROM p_params
  OR t.input_digest IS DISTINCT FROM p_digest
  OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
  OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
  OR t.deadline IS NULL OR t.deadline<=clock_timestamp() THEN RETURN NULL;END IF;
 SELECT * INTO s FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF s.id IS NULL OR s.fencing_token IS DISTINCT FROM p_fence OR s.resource_class IS DISTINCT FROM 'cpu'
  OR s.lease_until IS NULL OR s.lease_until<=clock_timestamp()
  OR photo.worker_authorized(p_tenant,p_worker,'cpu',s.node_id,t.operation,
   t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') IS DISTINCT FROM true
  OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run
  AND w.renderer_version='v2.2') THEN RETURN NULL;END IF;
 params:=p_json::jsonb;
 root_contract:='InterchangeArchive';
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT count(*) FROM jsonb_object_keys(params))<>4
 OR NOT(params ?& ARRAY['schema_version','request','max_expanded_bytes','max_files']) THEN RETURN NULL;END IF;
 IF jsonb_typeof(params->'request') IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(params->'request'))<>4
 OR NOT((params->'request') ?& ARRAY['project_id','direction','format','bundle'])
 OR params->'request'->>'direction' IS DISTINCT FROM 'import'
 OR params->'request'->>'format' IS DISTINCT FROM 'photoeditor_bundle'
 OR coalesce(params->>'max_expanded_bytes','') !~ '^[1-9][0-9]*$'
 OR coalesce(params->>'max_files','') !~ '^[1-9][0-9]*$' THEN RETURN NULL;END IF;
 expanded:=(params->>'max_expanded_bytes')::bigint;files:=(params->>'max_files')::integer;
 IF expanded NOT BETWEEN 1 AND 137438953472 OR files NOT BETWEEN 1 AND 30000 THEN RETURN NULL;END IF;
 root:=params->'request'->'bundle';
 IF jsonb_typeof(root) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(root))<>2
 OR coalesce(root->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR root->>'artifact_id'='00000000-0000-0000-0000-000000000000'
 OR coalesce(root->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND workflow_type='interchange' FOR SHARE;
 SELECT d.v30_interchange_proof INTO proof FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run
 AND d.owner_id=p_task AND d.artifact_id=p_params AND d.sha256=p_sha AND d.purpose='task_params' AND d.state='ready' FOR SHARE;
 IF w.id IS NULL OR proof IS NULL OR proof-'schema_version'-'auth_session_id'-'project_id'-'bundle'<>'{}'::jsonb
 OR proof->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR proof->>'project_id' IS DISTINCT FROM w.project_id::text
 OR params->'request'->>'project_id' IS DISTINCT FROM w.project_id::text
 OR proof->'bundle' IS DISTINCT FROM root
 OR photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,(proof->>'auth_session_id')::uuid) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id AND archived_at IS NULL AND download_allowed)
 OR NOT EXISTS(SELECT 1 FROM photo.snapshots sn JOIN photo.snapshot_items si ON si.tenant_id=sn.tenant_id AND si.snapshot_id=sn.id
 WHERE sn.tenant_id=p_tenant AND sn.id=w.input_snapshot_id AND sn.kind='v2' AND sn.expires_at IS NULL AND sn.item_count=1
 AND si.ordinal=0 AND si.asset_version_id IS NULL AND si.artifact_id=(root->>'artifact_id')::uuid AND si.sha256=root->>'sha256') THEN RETURN NULL;END IF;
 SELECT bytes INTO archive_bytes FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(root->>'artifact_id')::uuid AND sha256=root->>'sha256' FOR SHARE;
 estimate:=t.execution_profile->'resource_estimate';
 IF archive_bytes NOT BETWEEN 1 AND 68719476736 OR archive_bytes IS NULL
 OR coalesce((estimate->>'output_bytes')::numeric,0)<expanded+8388608
 OR coalesce((estimate->>'scratch_bytes')::numeric,0)<archive_bytes+expanded+8388608
 OR coalesce((estimate->>'ram_bytes')::numeric,0)<536870912
 OR estimate->'vram_bytes' IS DISTINCT FROM '0'::jsonb THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run
  AND d.owner_id=p_task AND d.purpose='task_params' AND d.state='ready'
  AND d.artifact_id=p_params AND d.sha256=p_sha AND d.bytes=octet_length(p_json)) THEN RETURN NULL;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract=root_contract)<>1
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.contract=root_contract AND i.artifact_id=(root->>'artifact_id')::uuid AND i.sha256=root->>'sha256')
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.contract='Params' AND i.artifact_id=p_params AND i.sha256=p_sha) THEN RETURN NULL;END IF;
 PERFORM a.id FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task ORDER BY a.id FOR SHARE OF a;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
  AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL
   OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN NULL;END IF;
 IF t.lease_until<=clock_timestamp() OR t.deadline<=clock_timestamp() OR s.lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('archive_bytes',archive_bytes,'expanded_bytes',expanded,'files',files);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_parse_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_parse_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_parse_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
