-- +goose Up
-- Read historical committed admission without reopening or dispatching work.
-- A null answer includes both absence and revoked authority.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_restoration_admission_ticket(p_tenant uuid,p_run uuid,
 p_actor uuid,p_session uuid,p_project uuid,p_shoot uuid,p_snapshot uuid,
 p_proposal text,p_profile text,
 p_source_observation uuid,p_donor_observation uuid) RETURNS jsonb LANGUAGE plpgsql VOLATILE
 SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 plan jsonb;body jsonb;task jsonb;origin jsonb;target jsonb;guard jsonb;intent jsonb;
 original record;c jsonb;proof text;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_proposal IS NULL OR octet_length(p_proposal) NOT BETWEEN 2 AND 65536
 OR p_profile IS NULL OR octet_length(p_profile) NOT BETWEEN 2 AND 65536
 OR p_project IS NULL OR p_shoot IS NULL OR p_snapshot IS NULL
 OR p_source_observation IS NULL OR p_donor_observation IS NULL
 OR p_source_observation=p_donor_observation THEN RETURN NULL;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
  AND kind='v2' AND created_by=p_actor AND graph_sealed
  AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v28_restoration_descriptor_jcs IS NULL
 OR r.v28_restoration_session_id IS DISTINCT FROM p_session
 OR r.input_snapshot_id IS DISTINCT FROM p_snapshot THEN RETURN NULL;END IF;
 plan:=r.v28_restoration_descriptor_jcs::jsonb;
 body:=plan->'params';task:=plan->'task';origin:=plan->'origin';
 IF task->>'operation' IS DISTINCT FROM 'portrait.glare'
 OR body->>'mode' IS DISTINCT FROM 'donor'
 OR (body-'confirmation') IS DISTINCT FROM (p_proposal::jsonb-'confirmation')
 OR task->'execution_profile' IS DISTINCT FROM p_profile::jsonb
 THEN RETURN NULL;END IF;
 IF plan->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR task->>'operation' NOT IN('portrait.glare','image.reflection')
 OR task->>'operation_version' IS DISTINCT FROM '1'
 OR task->>'resource_class' IS DISTINCT FROM 'gpu'
 THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
  AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview'
  AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR w.project_id IS DISTINCT FROM p_project THEN RETURN NULL;END IF;
 intent:=r.config_snapshot->'v28_restoration';
 IF jsonb_typeof(intent) IS DISTINCT FROM 'object'
 OR intent->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR intent->>'operation' IS DISTINCT FROM 'portrait.glare'
 OR intent->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR intent->>'project_id' IS DISTINCT FROM p_project::text
 OR intent->>'snapshot_id' IS DISTINCT FROM p_snapshot::text
 OR intent->>'actor_id' IS DISTINCT FROM p_actor::text
 OR intent->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR intent->>'run_id' IS DISTINCT FROM p_run::text
 OR intent->>'workflow_id' IS DISTINCT FROM w.id::text
 OR intent->>'shoot_session_id' IS DISTINCT FROM p_shoot::text
 OR intent->>'source_observation_id' IS DISTINCT FROM p_source_observation::text
 OR intent->>'donor_observation_id' IS DISTINCT FROM p_donor_observation::text
 OR intent->>'ordinal' IS DISTINCT FROM split_part(task->>'node_key',':',2)
 OR intent->>'params_proposal_sha' IS DISTINCT FROM
    encode(sha256(convert_to(p_proposal,'UTF8')),'hex')
 OR intent->>'base_asset_version_id' IS DISTINCT FROM
    body->'base'->>'asset_version_id'
 OR intent->>'donor_asset_version_id' IS DISTINCT FROM
    body->'donor'->>'asset_version_id'
 OR intent->'qualification' IS DISTINCT FROM
    task->'execution_profile'->'restoration_qualification'
 OR w.policy_sha IS DISTINCT FROM intent->'qualification'->>'policy_sha'
 THEN RETURN NULL;END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND id=(task->>'id')::uuid FOR SHARE;
 IF NOT FOUND OR t.operation IS DISTINCT FROM task->>'operation'
 OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'gpu' OR t.cancel_requested
 OR t.node_key IS DISTINCT FROM task->>'node_key'
 OR t.params_artifact_id::text IS DISTINCT FROM task->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM task->>'input_digest'
 OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
    IS DISTINCT FROM task->'execution_profile'
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true
 OR photo.v28_restoration_inventory_digest_ready(t.id,t.params_artifact_id,
  task->>'params_sha',t.operation,t.input_digest,plan->'entries',body,
  task->'execution_profile'->'restoration_qualification') IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 IF plan->'entries' IS DISTINCT FROM
  (SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object(
   'artifact_id',artifact_id,'sha256',sha256),'contract',contract) ORDER BY ordinal)
   FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id
    AND contract NOT IN('InputClosureChunk','InputClosureManifest'))
 THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.backend_documents d JOIN photo.artifacts a
  ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=t.id
  AND d.purpose='task_params' AND d.state='ready'
  AND d.artifact_id=t.params_artifact_id AND d.sha256=task->>'params_sha'
  AND d.bytes BETWEEN 2 AND 65536
  AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.media_type='application/json' AND a.output_role='task_params'
  AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
  AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF d,a;
 IF NOT FOUND THEN RETURN NULL;END IF;
 target:=CASE t.operation WHEN 'portrait.glare' THEN body->'glasses_mask'
  ELSE body->'mask' END;guard:=body->'protected';
 SELECT * INTO original FROM photo.v28_manual_pair_origin(p_tenant,w.project_id,
  r.input_snapshot_id,p_actor,p_session,body->'base',
  (target->>'artifact_id')::uuid,target->>'sha256',
  (guard->>'artifact_id')::uuid,guard->>'sha256',
  (origin->'target_pixel'->>'artifact_id')::uuid,
  origin->'target_pixel'->>'sha256',
  (origin->'protected_pixel'->>'artifact_id')::uuid,
  origin->'protected_pixel'->>'sha256');
 IF NOT FOUND OR original.root_id::text IS DISTINCT FROM origin->'root'->>'artifact_id'
 OR original.root_sha IS DISTINCT FROM origin->'root'->>'sha256'
 OR original.params_id::text IS DISTINCT FROM origin->'params'->>'artifact_id'
 OR original.params_sha IS DISTINCT FROM origin->'params'->>'sha256'
 OR photo.lock_v28_restoration_qualification(p_tenant,w.project_id,p_actor,
  p_session,t.operation,task->'execution_profile'->'restoration_qualification')
  IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF t.operation='portrait.glare' THEN
  SELECT doc.* INTO d FROM photo.backend_documents doc JOIN photo.artifacts a
   ON a.tenant_id=doc.tenant_id AND a.id=doc.artifact_id
  WHERE doc.tenant_id=p_tenant AND doc.run_id=p_run
   AND doc.purpose='v28_donor_confirmation' AND doc.state='ready'
   AND doc.artifact_id=(body->'confirmation'->>'artifact_id')::uuid
   AND doc.sha256=body->'confirmation'->>'sha256'
   AND a.sha256=doc.sha256 AND a.bytes=doc.bytes AND a.bucket=doc.bucket
   AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.media_type='application/json' AND a.output_role='v28_donor_confirmation'
   AND a.object_version=doc.object_version AND a.object_version NOT IN('','null')
   AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||doc.artifact_id
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF doc,a;
  IF NOT FOUND THEN RETURN NULL;END IF;
  c:=d.v28_donor_issuer_context;proof:=c->>'proof_jcs';
  IF c->>'tenant_id' IS DISTINCT FROM p_tenant::text
  OR c->>'run_id' IS DISTINCT FROM p_run::text
  OR c->>'project_id' IS DISTINCT FROM w.project_id::text
  OR c->>'shoot_session_id' IS DISTINCT FROM p_shoot::text
  OR c->>'actor_id' IS DISTINCT FROM p_actor::text
  OR c->>'auth_session_id' IS DISTINCT FROM p_session::text
  OR c->>'source_observation_id' IS DISTINCT FROM p_source_observation::text
  OR c->>'donor_observation_id' IS DISTINCT FROM p_donor_observation::text
  OR c->'params'->'base' IS DISTINCT FROM body->'base'
  OR c->'params'->'donor' IS DISTINCT FROM body->'donor'
  OR c->'params'->'source_faces' IS DISTINCT FROM body->'source_faces'
  OR c->'params'->'donor_faces' IS DISTINCT FROM body->'donor_faces'
  OR c->'params'->'eyes_mask' IS DISTINCT FROM body->'glasses_mask'
  OR c->'params'->'protected' IS DISTINCT FROM body->'protected'
  OR c->'params'->'strength' IS DISTINCT FROM body->'strength'
  OR d.owner_id::text IS DISTINCT FROM c->'params'->'confirmation'->>'artifact_id'
  OR d.owner_id::text IS DISTINCT FROM p_proposal::jsonb->'confirmation'->>'artifact_id'
  OR d.owner_id IS DISTINCT FROM t.id
  OR c->'params'->'confirmation'->>'sha256' IS DISTINCT FROM d.sha256
  OR d.artifact_id::text IS DISTINCT FROM body->'confirmation'->>'artifact_id'
  OR proof IS NULL OR octet_length(proof) NOT BETWEEN 2 AND 65536
  OR d.bytes IS DISTINCT FROM octet_length(proof)::bigint
  OR encode(sha256(convert_to(proof,'UTF8')),'hex') IS DISTINCT FROM d.sha256
  OR photo.lock_v28_donor_session(p_tenant,w.project_id,
   (c->>'shoot_session_id')::uuid,p_actor,p_session,proof::jsonb,
   body->'source_faces',body->'donor_faces') IS DISTINCT FROM true
  THEN RETURN NULL;END IF;
 END IF;
 RETURN jsonb_build_object('run_id',p_run,'workflow_id',w.id,
  'task_id',t.id,'state','queued');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v28_restoration_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_restoration_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_restoration_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
