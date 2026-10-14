-- +goose Up
-- Scheduler-owned continuation shell. Never marks a task ready; closure activation is separate.
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_v30_interchange_import(p_tenant uuid,p_workflow uuid,p_parsed text,
 p_plan text,p_params text,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;plan_scope jsonb;w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;
 d photo.backend_documents%ROWTYPE;pd photo.backend_documents%ROWTYPE;old photo.tasks%ROWTYPE;
 predecessor photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;plan jsonb;entry jsonb;
 task uuid;pid uuid;plan_id uuid;profile jsonb;expected jsonb;reserved bigint;required bigint;capacity numeric;used numeric;size bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_task) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF capacity IS NULL THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange';
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 task:=photo.v30_uuid5(p_workflow,'v2.interchange.import:0');pid:=photo.v30_uuid5(task,'interchange:params:1');plan_id:=photo.v30_uuid5(task,'interchange:plan:1');
 plan_scope:=photo.v30_interchange_import_document_scope(p_tenant,p_workflow,p_parsed,plan_id,'v30_interchange_import_plan',p_plan);
 scope:=photo.v30_interchange_import_document_scope(p_tenant,p_workflow,p_parsed,pid,'task_params',p_params);
 IF scope IS NULL OR plan_scope IS NULL THEN RETURN false;END IF;
 SELECT * INTO pd FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=r.id AND owner_id=task AND purpose='v30_interchange_import_plan'
 AND artifact_id=plan_id AND sha256=plan_scope->>'sha' AND bytes=(plan_scope->>'bytes')::bigint AND state='ready';
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=r.id AND owner_id=task AND purpose='task_params'
 AND artifact_id=pid AND sha256=scope->>'sha' AND bytes=(scope->>'bytes')::bigint AND state='ready';
 IF pd.id IS NULL OR d.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=pid
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 SELECT * INTO predecessor FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND operation='v2.interchange.parse'
 AND state='succeeded' AND NOT cancel_requested FOR SHARE;
 IF predecessor.id IS NULL OR photo.v30_interchange_parse_adoption_committed(p_tenant,predecessor.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 plan:=p_plan::jsonb;
 -- Bound every report/shard/root and the exact already-prepared recipe bodies.
 required:=(2+(jsonb_array_length(plan->'entries')+99)/100)::bigint*8388608;
 FOR entry IN SELECT value FROM jsonb_array_elements(plan->'entries') LOOP
  IF entry->'recipe'<>'null'::jsonb THEN
   SELECT bytes INTO size FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(entry#>>'{recipe,artifact_id}')::uuid
    AND sha256=entry#>>'{recipe,sha256}' AND state='ready' AND bytes BETWEEN 2 AND 8388608;
   IF size IS NULL THEN RETURN false;END IF;
   required:=required+size;
  END IF;
 END LOOP;
 profile:=p_task->'execution_profile';reserved:=(profile#>>'{resource_estimate,output_bytes}')::bigint;
 IF profile->>'backend' IS DISTINCT FROM 'cpu' OR profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR profile#>'{resource_estimate,vram_bytes}' IS DISTINCT FROM '0'::jsonb
 OR coalesce((profile#>>'{resource_estimate,ram_bytes}')::bigint,0)<1073741824
 OR coalesce((profile#>>'{resource_estimate,scratch_bytes}')::bigint,-1)<0
 OR reserved IS NULL OR reserved<required OR reserved>9007199254740991
 OR coalesce(p_task->>'input_digest','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 expected:=jsonb_build_object('id',task,'node_key','v2.interchange.import:0','operation','v2.interchange.import','operation_version','1',
  'resource_class','cpu','params_artifact_id',pid,'params_sha',d.sha256,'input_digest',p_task->>'input_digest','execution_profile',profile);
 IF p_task IS DISTINCT FROM expected THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=r.id FOR UPDATE;
 IF q.id IS NULL OR q.state<>'reserved' OR q.expires_at<=clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=task FOR UPDATE;
 IF FOUND THEN RETURN old.run_id=r.id AND old.state='blocked' AND old.attempt=0 AND NOT old.cancel_requested
 AND old.node_key='v2.interchange.import:0' AND old.operation='v2.interchange.import' AND old.operation_version='1'
 AND old.resource_class='cpu' AND old.params_artifact_id=pid AND old.input_digest=p_task->>'input_digest'
 AND (old.execution_profile=profile OR (old.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha'-'contracts_sha'=profile AND coalesce(old.execution_profile->>'contracts_sha','') ~ '^[0-9a-f]{64}$' AND photo.v2_input_closure_ready(task))) AND q.bytes>=reserved
 AND EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=r.id AND prerequisite_id=predecessor.id AND dependent_id=task AND condition='success');END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF used IS NULL OR used+reserved>capacity OR used+reserved>9007199254740991 OR q.bytes::numeric+reserved>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(task,p_tenant,r.id,'v2.interchange.import:0','v2.interchange.import','1','cpu','blocked','[]'::jsonb,pid,p_task->>'input_digest',profile);
 INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES(p_tenant,r.id,predecessor.id,task,'success');
 UPDATE photo.quota_reservations SET bytes=bytes+reserved WHERE id=q.id;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+reserved WHERE tenant_id=p_tenant;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.materialize_v30_interchange_import(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_v30_interchange_import(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.materialize_v30_interchange_import(uuid,uuid,text,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
