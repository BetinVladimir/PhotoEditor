-- +goose Up
-- Read only the exact sealed transport while current descriptor authority and
-- immutable manifest/chunk versions still match the blocked task.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_restoration_registered_transport(p_tenant uuid,p_run uuid,
 p_task uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;plan jsonb;entry jsonb;
 manifest jsonb;chunks jsonb;body text:='[';separator text:='';sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor
 AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v28_restoration_descriptor_jcs IS NULL
 OR r.v28_restoration_session_id IS DISTINCT FROM p_session THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF NOT FOUND OR photo.v28_restoration_registered_inventory_ready(p_tenant,p_run,p_task) IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 plan:=r.v28_restoration_descriptor_jcs::jsonb;
 FOR entry IN SELECT value FROM jsonb_array_elements(plan->'entries') LOOP
  body:=body||separator||'{"contract":"'||(entry->>'contract')||
   '","ref":{"artifact_id":"'||(entry->'ref'->>'artifact_id')||
   '","sha256":"'||(entry->'ref'->>'sha256')||'"}}';separator:=',';
 END LOOP;
 sha:=encode(sha256(convert_to(body||']','UTF8')),'hex');
 IF t.execution_profile->>'v2_entries_sha' IS DISTINCT FROM sha
 OR t.execution_profile->'v2_entry_count' IS DISTINCT FROM
  to_jsonb(jsonb_array_length(plan->'entries')) THEN RETURN NULL;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task
  AND contract='InputClosureManifest')<>1
 OR (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task
  AND contract='InputClosureChunk')<>1 THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.contract IN('InputClosureManifest','InputClosureChunk') AND NOT EXISTS(
  SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
   ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_task
  AND d.artifact_id=i.artifact_id AND d.sha256=i.sha256
  AND d.purpose=CASE i.contract WHEN 'InputClosureManifest' THEN 'v2_input_manifest'
   ELSE 'v2_input_chunk_0' END AND d.state='ready'
  AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
  AND a.bytes BETWEEN 2 AND 1048576 AND a.bucket=d.bucket
  AND a.media_type='application/json' AND a.output_role=d.purpose
  AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||a.id
  AND a.object_version=d.object_version AND a.object_version IS NOT NULL
  AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF d,a))
 THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO manifest
 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task
 AND contract='InputClosureManifest';
 SELECT jsonb_agg(jsonb_build_object('artifact_id',artifact_id,'sha256',sha256)
  ORDER BY ordinal) INTO chunks FROM photo.v2_task_inputs
 WHERE tenant_id=p_tenant AND task_id=p_task AND contract='InputClosureChunk';
 IF t.execution_profile->'v2_input_manifest' IS DISTINCT FROM manifest THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('manifest',manifest,'chunks',chunks,
  'count',jsonb_array_length(plan->'entries'),'sha256',sha);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v28_restoration_registered_transport(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_restoration_registered_transport(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_restoration_registered_transport(uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- The generic graph seal must not bypass restoration-specific checks.
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_v28_restoration;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_v28_restoration(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
  AND v28_restoration_descriptor_jcs IS NOT NULL)
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND operation IN('portrait.glare','image.reflection')) THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v28_restoration(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.seal_v28_restoration_graph(p_tenant uuid,p_run uuid,
 p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;task jsonb;body jsonb;origin jsonb;
 target jsonb;guard jsonb;task_id uuid;project_id uuid;original record;issuer jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
 AND created_by=p_actor FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed
 OR r.cancel_requested OR r.v28_restoration_descriptor_jcs IS NULL
 OR r.v28_restoration_session_id IS DISTINCT FROM p_session THEN RETURN false;END IF;
 plan:=r.v28_restoration_descriptor_jcs::jsonb;task:=plan->'task';body:=plan->'params';origin:=plan->'origin';
 task_id:=(task->>'id')::uuid;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR photo.read_v28_restoration_registered_transport(p_tenant,p_run,task_id,p_actor,p_session) IS NULL
 THEN RETURN false;END IF;
 SELECT w.project_id INTO project_id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
 AND w.run_id=p_run AND w.input_snapshot_id=r.input_snapshot_id
 AND w.workflow_type='preview' AND w.renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 target:=CASE task->>'operation' WHEN 'portrait.glare' THEN body->'glasses_mask' ELSE body->'mask' END;
 guard:=body->'protected';
 SELECT * INTO original FROM photo.v28_manual_pair_origin(p_tenant,project_id,r.input_snapshot_id,
  p_actor,p_session,body->'base',(target->>'artifact_id')::uuid,target->>'sha256',
  (guard->>'artifact_id')::uuid,guard->>'sha256',
  (origin->'target_pixel'->>'artifact_id')::uuid,origin->'target_pixel'->>'sha256',
  (origin->'protected_pixel'->>'artifact_id')::uuid,origin->'protected_pixel'->>'sha256');
 IF NOT FOUND OR original.root_id::text IS DISTINCT FROM origin->'root'->>'artifact_id'
 OR original.root_sha IS DISTINCT FROM origin->'root'->>'sha256'
 OR original.params_id::text IS DISTINCT FROM origin->'params'->>'artifact_id'
 OR original.params_sha IS DISTINCT FROM origin->'params'->>'sha256'
 OR photo.lock_v28_restoration_qualification(p_tenant,project_id,p_actor,p_session,
  task->>'operation',task->'execution_profile'->'restoration_qualification') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 IF task->>'operation'='portrait.glare' THEN
  IF photo.v28_donor_confirmation_scope(p_tenant,project_id,
   (body->'base'->>'asset_version_id')::uuid,
   (body->'confirmation'->>'artifact_id')::uuid,body->'confirmation'->>'sha256',
   p_actor,p_session,(body->'base'->'source'->>'artifact_id')::uuid,
   body->'base'->'source'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
  SELECT d.v28_donor_issuer_context INTO issuer FROM photo.backend_documents d
  WHERE d.tenant_id=p_tenant AND d.artifact_id=(body->'confirmation'->>'artifact_id')::uuid
  AND d.sha256=body->'confirmation'->>'sha256' AND d.purpose='v28_donor_confirmation'
  AND d.state='ready' FOR SHARE;
  IF NOT FOUND OR issuer->'params'->'donor' IS DISTINCT FROM body->'donor'
   OR issuer->'params'->'base' IS DISTINCT FROM body->'base'
   THEN RETURN false;END IF;
 END IF;
 -- Generic registry, quota, DAG, activation, event and outbox checks still run.
 RETURN photo.seal_v2_graph_pre_v28_restoration(p_tenant,p_run);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.seal_v28_restoration_graph(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v28_restoration_graph(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v28_restoration_graph(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
