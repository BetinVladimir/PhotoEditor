-- +goose Up
-- Blocked export shells only; native resource qualification remains an admission gate.
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_v30_interchange_export(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_params_body text,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;scope jsonb;plan_scope jsonb;frozen jsonb;plan jsonb;estimate jsonb;ram bigint;scratch bigint;json_limit bigint;contracts text;rid uuid;task_id uuid;param_id uuid;profile jsonb;expected jsonb;expanded bigint;files integer;archive_bytes bigint;output_bytes bigint;capacity numeric;used numeric;old photo.tasks%ROWTYPE;d photo.backend_documents%ROWTYPE;
BEGIN
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='Interchange.Export.Pending' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 IF saved IS NULL OR jsonb_typeof(p_task) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 frozen:=saved->'frozen';
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);
 task_id:=photo.v30_uuid5(photo.v30_uuid5(rid,'workflow'),'v2.interchange.export:0');
 param_id:=photo.v30_uuid5(task_id,'interchange:params:1');
 scope:=photo.v30_interchange_export_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,param_id,'task_params',p_params_body);
 IF scope IS NULL THEN RETURN false;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF capacity IS NULL THEN RETURN false;END IF;
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=rid
 AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested FOR UPDATE OF r;
 IF NOT FOUND THEN RETURN false;END IF;
 plan_scope:=photo.v30_interchange_export_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,
 (frozen->'plan'->>'artifact_id')::uuid,'v30_interchange_export_plan',frozen->>'plan_jcs');
 IF plan_scope IS NULL THEN RETURN false;END IF;
 PERFORM b.artifact_id FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=task_id AND b.purpose='v30_interchange_export_plan'
 AND b.artifact_id=(frozen->'plan'->>'artifact_id')::uuid AND b.state='ready'
 AND b.sha256=plan_scope->>'sha' AND b.bytes=(plan_scope->>'bytes')::bigint
 AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
 AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.output_role=b.purpose AND a.key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||b.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=task_id AND b.purpose='task_params' AND b.artifact_id=param_id
 AND b.state='ready' AND b.sha256=scope->>'sha' AND b.bytes=(scope->>'bytes')::bigint
 AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
 AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.output_role='task_params' AND a.key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||param_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF NOT FOUND THEN RETURN false;END IF;
 plan:=(frozen->>'plan_jcs')::jsonb;
 archive_bytes:=(plan->>'max_archive_bytes')::bigint;expanded:=(plan->>'max_expanded_bytes')::bigint;files:=(plan->>'max_files')::integer;
 IF archive_bytes IS NULL OR archive_bytes NOT BETWEEN 1 AND 68719476736 OR expanded IS NULL
 OR expanded NOT BETWEEN 1 AND 137438953472 OR files IS NULL OR files NOT BETWEEN 1 AND 30000 THEN RETURN false;END IF;
 estimate:=p_task->'execution_profile'->'resource_estimate';
 ram:=(estimate->>'ram_bytes')::bigint;scratch:=(estimate->>'scratch_bytes')::bigint;output_bytes:=(estimate->>'output_bytes')::bigint;
 json_limit:=(p_task->'execution_profile'->>'max_input_json_bytes')::bigint;contracts:=p_task->'execution_profile'->>'contracts_sha';
 -- Protocol floors only. Independent native qualification is required before
 -- sealing/admission; this function creates a blocked task and cannot dispatch.
 IF ram IS NULL OR scratch IS NULL OR output_bytes IS NULL OR json_limit IS NULL OR contracts IS NULL
 OR json_limit NOT BETWEEN 1 AND 134217728 OR ram NOT BETWEEN json_limit AND 9007199254740991
 OR scratch NOT BETWEEN archive_bytes AND 9007199254740991
 OR output_bytes NOT BETWEEN archive_bytes+16777216 AND 9007199254740991
 OR contracts !~ '^[0-9a-f]{64}$' OR coalesce(p_task->>'input_digest','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 profile:=jsonb_build_object('backend','cpu','renderer_version','v2.2','contracts_sha',contracts,'max_input_json_bytes',json_limit,
 'resource_estimate',jsonb_build_object('ram_bytes',ram,'vram_bytes',0,'scratch_bytes',scratch,'output_bytes',output_bytes,'pixel_count',0));
 expected:=jsonb_build_object('id',task_id,'node_key','v2.interchange.export:0','operation','v2.interchange.export','operation_version','1',
 'resource_class','cpu','params_artifact_id',param_id,'params_sha',d.sha256,'input_digest',p_task->>'input_digest','execution_profile',profile);
 IF p_task IS DISTINCT FROM expected THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=task_id FOR UPDATE;
 IF FOUND THEN RETURN old.run_id=rid AND old.state='blocked' AND old.attempt=0 AND NOT old.cancel_requested
 AND old.node_key='v2.interchange.export:0' AND old.operation='v2.interchange.export' AND old.operation_version='1'
 AND old.resource_class='cpu' AND old.params_artifact_id=param_id AND old.input_digest=p_task->>'input_digest'
 AND (old.execution_profile=profile OR (old.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha'=profile AND photo.v2_input_closure_ready(task_id)))
 AND EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=rid
 AND principal_type='user' AND principal_id=p_actor AND state='reserved' AND bytes=output_bytes AND expires_at>clock_timestamp());END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid)
 OR EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=rid) THEN RETURN false;END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF used IS NULL OR used+output_bytes>capacity OR used+output_bytes>9007199254740991 THEN RETURN false;END IF;
 UPDATE photo.backend_documents SET v30_interchange_proof=jsonb_build_object('schema_version',1,'direction','export','auth_session_id',p_session,'actor_id',p_actor,'key',p_key,'request_sha',p_request_sha) WHERE tenant_id=p_tenant AND id=d.id;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(task_id,p_tenant,rid,'v2.interchange.export:0','v2.interchange.export','1','cpu','blocked','[]'::jsonb,param_id,p_task->>'input_digest',profile);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',p_actor,'run',rid,output_bytes,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+output_bytes WHERE tenant_id=p_tenant;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.materialize_v30_interchange_export(uuid,uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_v30_interchange_export(uuid,uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.materialize_v30_interchange_export(uuid,uuid,uuid,text,text,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
