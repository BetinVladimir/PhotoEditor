-- Learning fit parameters belong to the deterministic initial task, and can
-- be published only after the training snapshot has been sealed.
-- +goose Up
ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check
 CHECK (purpose IN ('task_params','review_selection','v2_input_manifest',
  'v2_session_plan_params','v2_analysis_input_snapshot','v2_analysis_context',
  'v2_analysis_consents','v23_training_manifest','v23_training_snapshot',
  'v23_fit_params') OR
  CASE WHEN purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$'
   THEN substring(purpose from 16)::integer BETWEEN 0 AND 1953 ELSE false END OR
  CASE WHEN purpose ~ '^v2_analysis_describe_params_([0-9]|[1-3][0-9])$'
   THEN true ELSE false END);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v23_training_document_scope(p_tenant uuid,
 p_run uuid,p_owner uuid,p_purpose text,p_sha text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
 w photo.v2_workflows%ROWTYPE; h bytea; expected_task uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_owner IS NULL OR
  p_purpose NOT IN ('v23_training_manifest','v23_training_snapshot',
   'v23_fit_params') OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.workflow_type='learning'
  AND x.renderer_version='v2.2';
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'draft' OR
  r.graph_sealed OR r.cancel_requested OR q.state<>'reserved' OR
  q.expires_at<=clock_timestamp() OR w.id IS NULL OR
  NOT EXISTS(SELECT 1 FROM photo.tenants t
   WHERE t.id=p_tenant AND t.status='active') OR
  NOT EXISTS(SELECT 1 FROM photo.learning_profiles p
   WHERE p.tenant_id=p_tenant AND p.project_id=w.project_id
    AND p.id::text=r.config_snapshot->'v23_training'->>'profile_id')
  THEN RETURN false; END IF;
 IF p_purpose='v23_training_manifest' THEN
  RETURN p_owner::text=r.config_snapshot->'v23_training'->>'profile_id'
   AND p_sha=r.config_snapshot->'v23_training'->>'manifest_sha';
 END IF;
 IF p_purpose='v23_training_snapshot' THEN
  RETURN p_owner=p_run AND
   p_sha=r.config_snapshot->'v23_training'->>'snapshot_body_sha';
 END IF;
 h:=public.digest(uuid_send(w.id)||convert_to('learning.fit:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_task:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 RETURN p_owner=expected_task AND EXISTS(SELECT 1 FROM photo.snapshots s
  JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id
   AND i.snapshot_id=s.id AND i.ordinal=0
  WHERE s.tenant_id=p_tenant AND s.id=r.input_snapshot_id
   AND s.kind='v2' AND s.expires_at IS NULL AND s.item_count=1
   AND s.digest=r.config_snapshot->'v23_training'->>'snapshot_digest'
   AND i.sha256=r.config_snapshot->'v23_training'->>'manifest_sha');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_document_scope(uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_document_scope(uuid,uuid,uuid,text,text)
 FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.stage_v23_learning_fit(p_tenant uuid,p_run uuid,
 p_task uuid,p_params uuid,p_params_sha text,p_input_digest text,
 p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 d photo.backend_documents%ROWTYPE; existing photo.tasks%ROWTYPE;
 h bytea; expected uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_task IS NULL OR
  p_params IS NULL OR p_params_sha IS NULL OR
  p_params_sha !~ '^[0-9a-f]{64}$' OR p_input_digest IS NULL OR
  p_input_digest !~ '^[0-9a-f]{64}$' OR
  jsonb_typeof(p_profile) IS DISTINCT FROM 'object' OR
  p_profile->>'backend' IS DISTINCT FROM 'cpu' OR
  p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' OR
  jsonb_typeof(p_profile->'resource_estimate') IS DISTINCT FROM 'object' OR
  ((p_profile->'resource_estimate')-'ram_bytes'-'vram_bytes'-
   'scratch_bytes'-'output_bytes'-'pixel_count')<>'{}'::jsonb OR
  NOT (p_profile->'resource_estimate' ?& ARRAY[
   'ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count']) OR
  EXISTS(SELECT 1 FROM jsonb_each(p_profile->'resource_estimate') x
   WHERE jsonb_typeof(x.value)<>'number' OR
    x.value::text !~ '^(0|[1-9][0-9]{0,15})$') OR
  (p_profile->'resource_estimate'->>'ram_bytes')::bigint<536870912 OR
  (p_profile->'resource_estimate'->>'ram_bytes')::bigint>8589934592 OR
  (p_profile->'resource_estimate'->>'vram_bytes')::bigint<>0 OR
  (p_profile->'resource_estimate'->>'scratch_bytes')::bigint<67108864 OR
  (p_profile->'resource_estimate'->>'output_bytes')::bigint<8388608 OR
  (p_profile->'resource_estimate'->>'pixel_count')::bigint<>0
  THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant
  AND x.id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.workflow_type='learning';
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'draft' OR
  r.graph_sealed OR r.cancel_requested OR w.id IS NULL OR
  w.renderer_version<>'v2.2' OR w.input_snapshot_id<>r.input_snapshot_id OR
  NOT EXISTS(SELECT 1 FROM photo.snapshots s
   JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id
    AND i.snapshot_id=s.id AND i.ordinal=0
   WHERE s.tenant_id=p_tenant AND s.id=r.input_snapshot_id
    AND s.kind='v2' AND s.expires_at IS NULL AND s.item_count=1
    AND s.digest=r.config_snapshot->'v23_training'->>'snapshot_digest'
    AND i.sha256=r.config_snapshot->'v23_training'->>'manifest_sha') OR
  NOT EXISTS(SELECT 1 FROM photo.quota_reservations q
   WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
    AND q.state='reserved' AND q.expires_at>clock_timestamp()) OR
  NOT EXISTS(SELECT 1 FROM photo.operation_registry op
   WHERE op.operation='learning.fit' AND op.version='1'
    AND op.resource_class='cpu' AND op.backends ? 'cpu')
  THEN RETURN false; END IF;
 h:=public.digest(uuid_send(w.id)||convert_to('learning.fit:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_task<>expected THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.backend_documents x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.owner_id=expected AND x.purpose='v23_fit_params'
  FOR SHARE;
 IF d.id IS NULL OR d.state<>'ready' OR d.artifact_id<>p_params OR
  d.sha256<>p_params_sha OR NOT EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND
    a.sha256=d.sha256 AND a.state='ready' AND
    a.object_version=d.object_version) THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.id<>expected) OR
  EXISTS(SELECT 1 FROM photo.task_edges e WHERE e.tenant_id=p_tenant
   AND e.run_id=p_run) THEN RETURN false; END IF;
 SELECT * INTO existing FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.id=expected FOR UPDATE;
 IF existing.id IS NOT NULL THEN
  RETURN existing.node_key='learning.fit:0' AND
   existing.operation='learning.fit' AND
   existing.operation_version='1' AND existing.resource_class='cpu' AND
   existing.state='blocked' AND existing.attempt=0 AND
   NOT existing.cancel_requested AND existing.params_artifact_id=p_params AND
   existing.input_digest=p_input_digest AND
   existing.execution_profile=p_profile;
 END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,
  operation_version,resource_class,inputs,params_artifact_id,
  input_digest,execution_profile)
 VALUES(expected,p_tenant,p_run,'learning.fit:0','learning.fit','1','cpu',
  '[]'::jsonb,p_params,p_input_digest,p_profile);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v23_learning_fit(uuid,uuid,uuid,uuid,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v23_learning_fit(uuid,uuid,uuid,uuid,text,text,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v23_learning_fit(uuid,uuid,uuid,uuid,text,text,jsonb)
 TO photo_scheduler;
