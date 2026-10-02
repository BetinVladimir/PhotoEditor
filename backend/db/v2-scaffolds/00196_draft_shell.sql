-- Create a draft shell before publishing task-scoped input transport objects.
-- This function is called only by the trusted V2 compiler after request auth;
-- it rechecks membership, project access, snapshot ownership and storage quota.
-- A failed call is atomic. Replays use the admission idempotency record, not a
-- second shell insert with the same run UUID.
-- +goose StatementBegin
CREATE FUNCTION photo.create_v2_draft_shell(
 p_tenant uuid,p_run uuid,p_workflow_id uuid,p_project uuid,p_snapshot uuid,p_actor uuid,
 p_workflow text,p_policy_sha text,p_config_sha text,p_config jsonb,
 p_tasks jsonb,p_edges jsonb,p_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant_row photo.tenants%ROWTYPE; usage_row photo.tenant_usage%ROWTYPE;
 project_row photo.projects%ROWTYPE; member_row photo.memberships%ROWTYPE;
 item jsonb; edge jsonb; task_id uuid; source_id uuid; target_id uuid;
 task_count integer; edge_count integer; limit_bytes numeric;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_workflow_id IS NULL OR p_project IS NULL OR p_snapshot IS NULL OR p_actor IS NULL
  OR p_policy_sha IS NULL OR p_policy_sha !~ '^[0-9a-f]{64}$'
  OR p_config_sha IS NULL OR p_config_sha !~ '^[0-9a-f]{64}$'
  OR p_workflow IS NULL OR p_workflow NOT IN
   ('session','learning','capture','semantic','delivery','interchange','provenance','preview')
  OR jsonb_typeof(p_config) IS DISTINCT FROM 'object'
  OR p_config->>'project_id' IS DISTINCT FROM p_project::text
  OR jsonb_typeof(p_tasks) IS DISTINCT FROM 'array'
  OR jsonb_typeof(p_edges) IS DISTINCT FROM 'array'
  OR p_bytes IS NULL OR p_bytes NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false; END IF;
 task_count:=jsonb_array_length(p_tasks); edge_count:=jsonb_array_length(p_edges);
 IF task_count NOT BETWEEN 1 AND 5000 OR edge_count>20000
  OR pg_column_size(p_tasks)>8388608 OR pg_column_size(p_edges)>2097152 THEN RETURN false; END IF;
 SELECT * INTO tenant_row FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 SELECT * INTO usage_row FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO project_row FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project;
 SELECT * INTO member_row FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor;
 IF tenant_row.id IS NULL OR tenant_row.status<>'active' OR usage_row.id IS NULL
  OR project_row.id IS NULL OR project_row.archived_at IS NOT NULL
  OR member_row.id IS NULL OR member_row.status<>'active'
  OR member_row.role NOT IN ('owner','admin','editor')
  OR (project_row.visibility='restricted' AND member_row.role NOT IN ('owner','admin')
   AND NOT EXISTS(SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant
    AND acl.project_id=p_project AND acl.user_id=p_actor AND acl.role='editor'))
  OR NOT EXISTS(SELECT 1 FROM photo.snapshots s WHERE s.tenant_id=p_tenant AND s.id=p_snapshot
   AND s.kind='v2' AND s.principal_type='user' AND s.principal_id=p_actor
   AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp()))
  OR EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run) THEN RETURN false; END IF;
 limit_bytes:=(tenant_row.quotas->>'storage_bytes')::numeric;
 IF limit_bytes IS NULL OR usage_row.storage_bytes::numeric+usage_row.reserved_bytes+p_bytes>limit_bytes
  OR usage_row.storage_bytes::numeric+usage_row.reserved_bytes+p_bytes>9007199254740991 THEN RETURN false; END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object'
   OR (item-'id'-'node_key'-'operation'-'operation_version'-'resource_class'
    -'params_artifact_id'-'params_sha'-'input_digest'-'execution_profile')<>'{}'::jsonb
   OR coalesce(item->>'id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(item->>'params_artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(item->>'params_sha','') !~ '^[0-9a-f]{64}$'
   OR coalesce(item->>'input_digest','') !~ '^[0-9a-f]{64}$'
   OR length(coalesce(item->>'node_key','')) NOT BETWEEN 1 AND 256
   OR length(coalesce(item->>'operation','')) NOT BETWEEN 1 AND 120
   OR length(coalesce(item->>'operation_version','')) NOT BETWEEN 1 AND 64
   OR jsonb_typeof(item->'execution_profile') IS DISTINCT FROM 'object'
   OR item->'execution_profile' ? 'v2_input_manifest'
   OR coalesce(item->'execution_profile'->>'backend','') NOT IN ('cpu','cuda','mps')
   OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=item->>'operation'
    AND op.version=item->>'operation_version' AND op.resource_class=item->>'resource_class'
    AND op.backends ? (item->'execution_profile'->>'backend'))
   OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
    AND a.id=(item->>'params_artifact_id')::uuid AND a.sha256=item->>'params_sha'
    AND a.state='ready' AND a.media_type='application/json' AND a.object_version IS NOT NULL
    AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
   THEN RETURN false; END IF;
 END LOOP;
 FOR edge IN SELECT value FROM jsonb_array_elements(p_edges) LOOP
  IF jsonb_typeof(edge) IS DISTINCT FROM 'object'
   OR (edge-'prerequisite_id'-'dependent_id')<>'{}'::jsonb
   OR coalesce(edge->>'prerequisite_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(edge->>'dependent_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR edge->>'prerequisite_id'=edge->>'dependent_id' THEN RETURN false; END IF;
 END LOOP;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,
  config_hash,created_by,failure_policy,graph_sealed)
 VALUES(p_run,p_tenant,'v2','draft',p_snapshot,p_config,p_config_sha,p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,
  workflow_type,renderer_version,policy_sha)
 VALUES(p_workflow_id,p_tenant,p_project,p_run,p_snapshot,p_workflow,'v2.2',p_policy_sha);
 FOR item IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
  task_id:=(item->>'id')::uuid;
  INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,
   resource_class,inputs,params_artifact_id,input_digest,execution_profile)
  VALUES(task_id,p_tenant,p_run,item->>'node_key',item->>'operation',item->>'operation_version',
   item->>'resource_class','[]'::jsonb,(item->>'params_artifact_id')::uuid,
   item->>'input_digest',item->'execution_profile');
 END LOOP;
 FOR edge IN SELECT value FROM jsonb_array_elements(p_edges) LOOP
  source_id:=(edge->>'prerequisite_id')::uuid; target_id:=(edge->>'dependent_id')::uuid;
  IF NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=source_id)
   OR NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=target_id)
   THEN RAISE EXCEPTION 'V2_EDGE_OUTSIDE_GRAPH' USING ERRCODE='23514'; END IF;
  INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition)
  VALUES(p_tenant,p_run,source_id,target_id,'success');
 END LOOP;
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,
  holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',p_actor,'run',p_run,p_bytes,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_v2_draft_shell(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v2_draft_shell(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v2_draft_shell(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) TO photo_scheduler;
