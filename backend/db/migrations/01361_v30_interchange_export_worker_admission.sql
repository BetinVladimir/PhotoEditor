-- +goose Up
-- Worker lease authority; graph admission must independently qualify resources.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_export_ready(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;s photo.resource_slots%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.backend_documents%ROWTYPE;proof jsonb;saved jsonb;frozen jsonb;plan jsonb;params jsonb;estimate jsonb;archive_bytes bigint;reserved bigint;json_limit bigint;roots jsonb;
BEGIN
 IF p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 65536 OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_digest IS NULL OR p_digest !~ '^[0-9a-f]{64}$'
 OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker AND db_principal=session_user AND enabled) THEN RETURN NULL;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND workflow_type='interchange' AND renderer_version='v2.2';
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('queued','running') OR NOT r.graph_sealed OR r.cancel_requested
 OR t.id IS NULL OR t.id<>photo.v30_uuid5(w.id,'v2.interchange.export:0') OR t.state<>'running' OR t.cancel_requested
 OR t.operation<>'v2.interchange.export' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence OR t.worker_id IS DISTINCT FROM p_worker
 OR t.params_artifact_id IS DISTINCT FROM p_params OR p_params IS DISTINCT FROM photo.v30_uuid5(p_task,'interchange:params:1') OR t.input_digest IS DISTINCT FROM p_digest
 OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp() OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN NULL;END IF;
 SELECT * INTO s FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF s.id IS NULL OR s.fencing_token IS DISTINCT FROM p_fence OR s.resource_class<>'cpu' OR s.lease_until IS NULL OR s.lease_until<=clock_timestamp()
 OR photo.worker_authorized(p_tenant,p_worker,'cpu',s.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task AND purpose='task_params'
 AND artifact_id=p_params AND sha256=p_sha AND bytes=octet_length(p_json) AND state='ready' FOR SHARE;
 proof:=d.v30_interchange_proof;
 IF d.id IS NULL OR proof IS NULL OR proof IS DISTINCT FROM jsonb_build_object('schema_version',1,'direction','export','auth_session_id',proof->>'auth_session_id','actor_id',r.created_by,'key',proof->>'key','request_sha',r.config_snapshot->>'request_sha')
 OR photo.v30_uuid5(p_tenant,'interchange:create:'||r.created_by||':'||(proof->>'key')) IS DISTINCT FROM p_run
 OR photo.v25_lens_snapshot_ready(p_tenant,w.project_id,w.input_snapshot_id,r.created_by,(proof->>'auth_session_id')::uuid) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id AND archived_at IS NULL AND download_allowed) THEN RETURN NULL;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=r.created_by
 AND route='Interchange.Export.Pending' AND key=proof->>'key' AND request_hash=proof->>'request_sha' AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM r.created_by::text THEN RETURN NULL;END IF;
 frozen:=saved->'frozen';params:=p_json::jsonb;
 IF frozen->>'params_jcs' IS DISTINCT FROM p_json OR frozen->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params,'sha256',p_sha)
 OR frozen->>'request_sha256' IS DISTINCT FROM proof->>'request_sha' OR frozen->'request'->>'direction' IS DISTINCT FROM 'export'
 OR frozen->'request'->>'project_id' IS DISTINCT FROM w.project_id::text OR frozen->'request'->>'snapshot_id' IS DISTINCT FROM w.input_snapshot_id::text
 OR params IS DISTINCT FROM jsonb_build_object('schema_version',1,'plan',frozen->'plan','max_report_bytes',8388608) THEN RETURN NULL;END IF;
 plan:=(frozen->>'plan_jcs')::jsonb;
 IF frozen->'plan' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(p_task,'interchange:plan:1'),'sha256',encode(sha256(convert_to(frozen->>'plan_jcs','UTF8')),'hex'))
 OR plan->>'direction' IS DISTINCT FROM 'export' OR plan->>'project_id' IS DISTINCT FROM w.project_id::text
 OR plan->>'snapshot_digest' IS DISTINCT FROM r.config_snapshot->>'snapshot_digest' THEN RETURN NULL;END IF;
 roots:=jsonb_build_array(jsonb_build_object('artifact_id',p_params,'sha256',p_sha),frozen->'plan',frozen->'snapshot');
 IF t.inputs IS DISTINCT FROM (jsonb_build_array(t.execution_profile->'v2_input_manifest')||roots) THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='Params' AND artifact_id=p_params AND sha256=p_sha)
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InterchangePlan' AND artifact_id=(frozen->'plan'->>'artifact_id')::uuid AND sha256=frozen->'plan'->>'sha256')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InputSnapshot' AND artifact_id=(frozen->'snapshot'->>'artifact_id')::uuid AND sha256=frozen->'snapshot'->>'sha256') THEN RETURN NULL;END IF;
 PERFORM a.id FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task ORDER BY a.id FOR SHARE OF a;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL
 OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN NULL;END IF;
 estimate:=t.execution_profile->'resource_estimate';archive_bytes:=(plan->>'max_archive_bytes')::bigint;reserved:=(estimate->>'output_bytes')::bigint;
 json_limit:=(t.execution_profile->>'max_input_json_bytes')::bigint;
 IF archive_bytes IS NULL OR archive_bytes NOT BETWEEN 1 AND 68719476736 OR reserved IS NULL OR reserved NOT BETWEEN archive_bytes+16777216 AND 9007199254740991
 OR json_limit IS NULL OR json_limit NOT BETWEEN 1 AND 134217728 OR estimate->'vram_bytes' IS DISTINCT FROM '0'::jsonb
 OR coalesce((estimate->>'ram_bytes')::numeric,0)<json_limit OR coalesce((estimate->>'scratch_bytes')::numeric,0)<archive_bytes
 OR coalesce(t.execution_profile->>'contracts_sha','') !~ '^[0-9a-f]{64}$'
 OR NOT EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run AND state='reserved' AND bytes>=reserved AND expires_at>clock_timestamp()) THEN RETURN NULL;END IF;
 -- No lease may expire during the authority lookup.
 IF t.lease_until<=clock_timestamp() OR t.deadline<=clock_timestamp() OR s.lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('workflow_id',w.id,'project_id',w.project_id,'snapshot_digest',r.config_snapshot->>'snapshot_digest',
 'max_archive_bytes',archive_bytes,'max_output_bytes',reserved,'max_input_json_bytes',json_limit);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_export_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_export_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_export_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
