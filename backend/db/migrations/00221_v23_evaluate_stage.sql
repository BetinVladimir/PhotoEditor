-- Stage a blocked evaluation task only against the exact adopted candidate,
-- fenced expansion, immutable Params document and frozen training snapshot.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.stage_v23_learning_evaluate(p_tenant uuid,p_run uuid,
 p_expansion uuid,p_token uuid,p_task uuid,p_params uuid,p_params_sha text,
 p_candidate uuid,p_candidate_sha text,p_input_digest text,p_profile jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; e photo.graph_expansions%ROWTYPE;
 w photo.v2_workflows%ROWTYPE; d photo.backend_documents%ROWTYPE;
 existing photo.tasks%ROWTYPE; candidate record; h bytea; expected uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_task IS NULL OR p_params IS NULL OR
  p_candidate IS NULL OR p_params_sha IS NULL OR
  p_params_sha !~ '^[0-9a-f]{64}$' OR p_candidate_sha IS NULL OR
  p_candidate_sha !~ '^[0-9a-f]{64}$' OR p_input_digest IS NULL OR
  p_input_digest !~ '^[0-9a-f]{64}$' OR
  jsonb_typeof(p_profile) IS DISTINCT FROM 'object' OR
  p_profile->>'backend' IS DISTINCT FROM 'cuda' OR
  p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' OR
  jsonb_typeof(p_profile->'resource_estimate') IS DISTINCT FROM 'object' OR
  ((p_profile->'resource_estimate')-'ram_bytes'-'vram_bytes'-
   'scratch_bytes'-'output_bytes'-'pixel_count')<>'{}'::jsonb OR
  NOT (p_profile->'resource_estimate' ?& ARRAY[
   'ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count']) OR
  EXISTS(SELECT 1 FROM jsonb_each(p_profile->'resource_estimate') x
   WHERE jsonb_typeof(x.value)<>'number' OR
    x.value::text !~ '^(0|[1-9][0-9]{0,15})$') OR
  (p_profile->'resource_estimate'->>'ram_bytes')::bigint<1073741824 OR
  (p_profile->'resource_estimate'->>'ram_bytes')::bigint>34359738368 OR
  (p_profile->'resource_estimate'->>'vram_bytes')::bigint<2147483648 OR
  (p_profile->'resource_estimate'->>'vram_bytes')::bigint>12884901888 OR
  (p_profile->'resource_estimate'->>'scratch_bytes')::bigint<536870912 OR
  (p_profile->'resource_estimate'->>'output_bytes')::bigint<8388608 OR
  (p_profile->'resource_estimate'->>'pixel_count')::bigint<1 OR
  (p_profile->'resource_estimate'->>'pixel_count')::bigint>1000000000000
  THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant
  AND x.id=p_run FOR UPDATE;
 SELECT * INTO e FROM photo.graph_expansions x WHERE x.tenant_id=p_tenant
  AND x.id=p_expansion AND x.run_id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.workflow_type='learning';
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR
  r.graph_sealed OR r.cancel_requested OR e.id IS NULL OR
  e.kind<>'v2.learning.evaluate/1' OR e.state<>'open' OR
  e.expansion_token IS DISTINCT FROM p_token OR
  e.expansion_lease_until<=clock_timestamp() OR w.id IS NULL OR
  w.renderer_version<>'v2.2' OR w.input_snapshot_id<>r.input_snapshot_id OR
  NOT EXISTS(SELECT 1 FROM photo.tenants tenant WHERE tenant.id=p_tenant
   AND tenant.status='active') OR
  NOT EXISTS(SELECT 1 FROM photo.quota_reservations q
   WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
    AND q.state='reserved' AND q.expires_at>clock_timestamp()) OR
  NOT EXISTS(SELECT 1 FROM photo.snapshots s
   JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id
    AND i.snapshot_id=s.id AND i.ordinal=0
   WHERE s.tenant_id=p_tenant AND s.id=r.input_snapshot_id
    AND s.kind='v2' AND s.expires_at IS NULL AND s.item_count=1
    AND s.digest=r.config_snapshot->'v23_training'->>'snapshot_digest'
    AND i.sha256=r.config_snapshot->'v23_training'->>'manifest_sha') OR
  NOT EXISTS(SELECT 1 FROM photo.operation_registry op
   WHERE op.operation='learning.evaluate' AND op.version='1'
    AND op.resource_class='gpu' AND op.backends ? 'cuda')
  THEN RETURN false; END IF;
 h:=public.digest(uuid_send(w.id)||convert_to('learning.evaluate:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_task<>expected THEN RETURN false; END IF;
 SELECT * INTO candidate FROM photo.v23_learning_candidate_input(p_tenant,p_run);
 IF candidate.artifact_id IS NULL OR candidate.task_id<>e.parent_task_id OR
  candidate.artifact_id<>p_candidate OR candidate.sha256<>p_candidate_sha
  THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.backend_documents x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.owner_id=p_expansion
  AND x.purpose='v23_evaluate_params' FOR SHARE;
 IF d.id IS NULL OR d.state<>'ready' OR d.artifact_id<>p_params OR
  d.sha256<>p_params_sha OR NOT EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND
    a.sha256=d.sha256 AND a.state='ready' AND
    a.media_type='application/json' AND a.verified_at IS NOT NULL AND
    a.object_version=d.object_version AND
    (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
  THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.id NOT IN (e.parent_task_id,expected)) OR
  EXISTS(SELECT 1 FROM photo.task_edges edge WHERE edge.tenant_id=p_tenant
   AND edge.run_id=p_run AND
   (edge.prerequisite_id<>e.parent_task_id OR edge.dependent_id<>expected
    OR edge.condition<>'success')) THEN RETURN false; END IF;
 SELECT * INTO existing FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.id=expected FOR UPDATE;
 IF existing.id IS NOT NULL THEN
  RETURN existing.node_key='learning.evaluate:0' AND
   existing.operation='learning.evaluate' AND
   existing.operation_version='1' AND existing.resource_class='gpu' AND
   existing.state='blocked' AND existing.attempt=0 AND
   NOT existing.cancel_requested AND existing.params_artifact_id=p_params AND
   existing.input_digest=p_input_digest AND
   existing.execution_profile=p_profile AND
   (SELECT count(*) FROM photo.task_edges edge WHERE edge.tenant_id=p_tenant
    AND edge.run_id=p_run AND edge.prerequisite_id=e.parent_task_id
    AND edge.dependent_id=expected AND edge.condition='success')=1;
 END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,
  operation_version,resource_class,inputs,params_artifact_id,
  input_digest,execution_profile)
 VALUES(expected,p_tenant,p_run,'learning.evaluate:0','learning.evaluate','1',
  'gpu','[]'::jsonb,p_params,p_input_digest,p_profile);
 INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,
  condition) VALUES(p_tenant,p_run,e.parent_task_id,expected,'success');
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v23_learning_evaluate(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v23_learning_evaluate(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v23_learning_evaluate(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,jsonb)
 TO photo_scheduler;
