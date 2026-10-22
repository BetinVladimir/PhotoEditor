-- +goose Up
-- Current Reflection authority is independent of a running-worker lease.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_reflection_current_authority(p_tenant uuid,p_review uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;plan jsonb;body jsonb;origin jsonb;pin jsonb;original record;
BEGIN
 IF p_tenant IS NULL OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review AND kind='image.reflection'
  AND ((state='pending' AND revision=1) OR (state='decided' AND revision=2)) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=v.run_id AND kind='v2'
  AND status IN('waiting_review','completed') AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=v.task_id AND run_id=r.id
  AND operation='image.reflection' AND operation_version='1' AND state='succeeded' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id
  AND attempt=t.attempt AND fencing_token=t.fencing_token AND review_required FOR SHARE;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
  AND event_id=photo.v2_adoption_event_id(t.id,z.id)) THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND workflow_type='preview'
  AND renderer_version='v2.2' AND input_snapshot_id=r.input_snapshot_id FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 plan:=r.v28_restoration_descriptor_jcs::jsonb;body:=plan->'params';origin:=plan->'origin';pin:=t.execution_profile->'restoration_qualification';
 IF body IS NULL OR origin IS NULL OR pin IS NULL OR body->>'schema_version' IS DISTINCT FROM '1'
  OR plan->'task'->>'id' IS DISTINCT FROM t.id::text OR plan->'task'->>'operation' IS DISTINCT FROM 'image.reflection'
  OR plan->>'schema_version' IS DISTINCT FROM '1'
  OR t.resource_class IS DISTINCT FROM 'gpu'
  OR t.node_key IS DISTINCT FROM plan->'task'->>'node_key'
  OR t.id IS DISTINCT FROM photo.semantic_uuid_v5(w.id,t.node_key)
  OR t.params_artifact_id::text IS DISTINCT FROM plan->'task'->>'params_artifact_id'
  OR t.input_digest IS DISTINCT FROM plan->'task'->>'input_digest'
  OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM plan->'task'->'execution_profile'
  OR r.config_snapshot->'v28_restoration'->>'actor_id' IS DISTINCT FROM r.created_by::text
  OR r.config_snapshot->'v28_restoration'->>'auth_session_id' IS DISTINCT FROM r.v28_restoration_session_id::text
  OR r.config_snapshot->'v28_restoration'->>'base_asset_version_id' IS DISTINCT FROM body->'base'->>'asset_version_id'
  OR photo.v28_restoration_inventory_digest_ready(t.id,t.params_artifact_id,
   plan->'task'->>'params_sha',t.operation,t.input_digest,plan->'entries',body,pin) IS DISTINCT FROM true
  OR pin IS DISTINCT FROM plan->'task'->'execution_profile'->'restoration_qualification'
  OR pin->>'backend' IS DISTINCT FROM t.execution_profile->>'backend'
  OR pin->>'backend' NOT IN('cpu','cuda') OR pin->>'model_lock_sha' IS DISTINCT FROM body->>'model_lock_sha'
  OR pin->>'runtime_lock_sha' IS DISTINCT FROM t.execution_profile->>'restoration_runtime_sha'
  OR pin->'calibration' IS DISTINCT FROM body->'calibration'
  OR body->'base'->'source'->>'artifact_id' IS DISTINCT FROM v.base_artifact_id::text
  OR photo.lock_v28_restoration_qualification(p_tenant,w.project_id,p_actor,p_session,'image.reflection',pin) IS DISTINCT FROM true
  OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true
  OR plan->'entries' IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',artifact_id,'sha256',sha256),'contract',contract) ORDER BY ordinal) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id AND contract NOT IN('InputClosureChunk','InputClosureManifest'))
  OR NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.backend_documents d
   ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id AND d.run_id=r.id AND d.owner_id=t.id
   AND d.purpose='task_params' AND d.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
   AND a.bucket=d.bucket AND a.object_version=d.object_version
   WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
    AND a.sha256=plan->'task'->>'params_sha'
    AND a.sha256=encode(sha256(convert_to(r.v28_restoration_params_intent->>'params_jcs','UTF8')),'hex')
    AND (r.v28_restoration_params_intent->>'params_jcs')::jsonb=body
    AND a.bytes=octet_length(r.v28_restoration_params_intent->>'params_jcs') AND a.state='ready' AND a.verified_at IS NOT NULL
    AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 THEN RETURN false;END IF;
 SELECT * INTO original FROM photo.v28_manual_pair_origin(p_tenant,w.project_id,r.input_snapshot_id,
  r.created_by,r.v28_restoration_session_id,body->'base',
  (body->'mask'->>'artifact_id')::uuid,body->'mask'->>'sha256',
  (body->'protected'->>'artifact_id')::uuid,body->'protected'->>'sha256',
  (origin->'target_pixel'->>'artifact_id')::uuid,origin->'target_pixel'->>'sha256',
  (origin->'protected_pixel'->>'artifact_id')::uuid,origin->'protected_pixel'->>'sha256');
 IF NOT FOUND OR original.root_id::text IS DISTINCT FROM origin->'root'->>'artifact_id'
  OR original.root_sha IS DISTINCT FROM origin->'root'->>'sha256'
  OR original.params_id::text IS DISTINCT FROM origin->'params'->>'artifact_id'
  OR original.params_sha IS DISTINCT FROM origin->'params'->>'sha256' THEN RETURN false;END IF;
 RETURN EXISTS(SELECT FROM photo.artifacts a JOIN photo.v2_artifact_links link
  ON link.tenant_id=a.tenant_id AND link.workflow_id=w.id AND link.project_id=w.project_id
   AND link.artifact_id=a.id AND link.sha256=a.sha256
   AND link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
  WHERE a.tenant_id=p_tenant AND a.id=v.candidate_artifact_id AND a.sha256=v.candidate_hash
   AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
   AND a.output_role='working_image' AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_reflection_current_authority(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_reflection_current_authority(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_reflection_current_authority(uuid,uuid,uuid,uuid) TO photo_api;

DO $$ DECLARE definition text;needle text;BEGIN
 definition:=pg_get_functiondef('photo.get_v28_restoration_review_selection(uuid,uuid,uuid,uuid)'::regprocedure);
 needle:='AND selected.review_id=p_review';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'REFLECTION_SELECTION_AUTHORITY_ANCHOR';END IF;
 EXECUTE replace(definition,needle,needle||' AND (v.kind<>''image.reflection'' OR photo.v28_reflection_current_authority(p_tenant,p_review,p_actor,p_session))');
 definition:=pg_get_functiondef('photo.decide_v28_restoration_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)'::regprocedure);
 needle:='IF NOT authorized OR NOT photo.v28_restoration_review_decision_ready(p_tenant,p_review)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'REFLECTION_DECISION_AUTHORITY_ANCHOR';END IF;
 EXECUTE replace(definition,needle,'IF (v.kind=''image.reflection'' AND photo.v28_reflection_current_authority(p_tenant,p_review,p_actor,p_session) IS DISTINCT FROM true) OR NOT authorized OR NOT photo.v28_restoration_review_decision_ready(p_tenant,p_review)');
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v28_reflection_version(p_tenant uuid,p_review uuid,p_actor uuid,p_session uuid,
 p_decision_hash text,p_base text,p_image text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;src photo.asset_versions%ROWTYPE;old photo.asset_versions%ROWTYPE;
 selected record;body jsonb;base jsonb;image jsonb;identity uuid;next_no bigint;target_project uuid;
BEGIN
 IF p_tenant IS NULL OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_decision_hash IS NULL OR p_decision_hash !~ '^[0-9a-f]{64}$'
 OR p_base IS NULL OR octet_length(p_base) NOT BETWEEN 2 AND 1048576
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review
  AND kind='image.reflection' AND state='decided' AND revision=2;
 IF NOT FOUND THEN RETURN NULL;END IF;
 body:=(SELECT v28_restoration_descriptor_jcs::jsonb->'params' FROM photo.runs WHERE tenant_id=p_tenant AND id=v.run_id);
 SELECT * INTO src FROM photo.asset_versions WHERE tenant_id=p_tenant
  AND id=(body->'base'->>'asset_version_id')::uuid AND review_state='accepted';
 IF NOT FOUND OR src.master_artifact_id IS DISTINCT FROM v.base_artifact_id THEN RETURN NULL;END IF;
 SELECT w.project_id INTO target_project FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=v.run_id;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=target_project
  AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 -- Serialize version numbering before the authority reader acquires asset locks.
 PERFORM a.id FROM photo.assets a WHERE a.tenant_id=p_tenant AND a.id=src.asset_id
  AND a.project_id=target_project AND a.deleted_at IS NULL AND a.upload_state='verified' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO selected FROM photo.get_v28_restoration_review_selection(p_tenant,p_actor,p_session,p_review);
 IF NOT FOUND OR selected.decision_hash IS DISTINCT FROM p_decision_hash THEN RETURN NULL;END IF;
 base:=p_base::jsonb;image:=p_image::jsonb;
 IF jsonb_typeof(base) IS DISTINCT FROM 'object' OR jsonb_typeof(image) IS DISTINCT FROM 'object'
 OR encode(sha256(convert_to(p_base,'UTF8')),'hex') IS DISTINCT FROM src.content_hash
 OR base->>'dtype' IS DISTINCT FROM 'float32' OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR (base-'pixels') IS DISTINCT FROM (image-'pixels') THEN RETURN NULL;END IF;
 IF selected.route='skip' THEN RETURN NULL;END IF;
 IF selected.route='base' THEN
  IF selected.image_id=src.master_artifact_id AND selected.image_sha=src.content_hash
   AND p_image=p_base THEN RETURN src.id;END IF;
  RETURN NULL;
 END IF;
 IF selected.route IS DISTINCT FROM 'candidate'
 OR selected.image_id IS DISTINCT FROM v.candidate_artifact_id
 OR encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM selected.image_sha
 OR image->'pixels'->>'artifact_id' IS NULL
 OR image->'pixels'->>'artifact_id' IS NOT DISTINCT FROM base->'pixels'->>'artifact_id'
 THEN RETURN NULL;END IF;
 -- Child references must belong to this exact adopted result, not merely be
 -- ready artifacts elsewhere in the tenant. These links are also GC roots.
 IF EXISTS(SELECT 1 FROM (VALUES
  (image->'pixels','$/working_image/pixels'),
  (image->'alpha','$/working_image/alpha')) required(ref,path)
  WHERE required.ref IS NOT NULL AND required.ref<>'null'::jsonb AND NOT EXISTS(
   SELECT 1 FROM photo.v2_artifact_links link JOIN photo.v2_workflows workflow
    ON workflow.tenant_id=link.tenant_id AND workflow.id=link.workflow_id
   WHERE link.tenant_id=p_tenant AND workflow.run_id=v.run_id AND link.project_id=target_project
    AND link.artifact_id=(required.ref->>'artifact_id')::uuid
    AND link.sha256=required.ref->>'sha256'
    AND link.purpose=encode(sha256(convert_to('tasks/'||v.task_id::text||'/'||required.path,'UTF8')),'hex')))
 THEN RETURN NULL;END IF;
 -- All retained image children must still be ready exact-version objects.
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(
  (image->'pixels'->>'artifact_id')::uuid,(image->'alpha'->>'artifact_id')::uuid) ORDER BY a.id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=(image->'pixels'->>'artifact_id')::uuid AND a.sha256=image->'pixels'->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR (image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb AND NOT EXISTS(
  SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=(image->'alpha'->>'artifact_id')::uuid AND a.sha256=image->'alpha'->>'sha256'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN NULL;END IF;
 identity:=photo.semantic_uuid_v5(p_review,'V28.ReflectionAppliedVersion');
 SELECT * INTO old FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=identity;
 IF FOUND THEN
  IF old.asset_id=src.asset_id AND old.parent_asset_version_id=src.id
   AND old.master_artifact_id=selected.image_id AND old.content_hash=selected.image_sha
   AND old.metadata=image AND old.created_by_run_id=v.run_id AND old.review_state='accepted'
   AND EXISTS(SELECT 1 FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity
    AND source_version_id=src.id AND role='reflection_source' AND ordinal=0) THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 IF photo.v28_reflection_current_authority(p_tenant,p_review,p_actor,p_session) IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 SELECT coalesce(max(version_no),0)::bigint+1 INTO next_no FROM photo.asset_versions
  WHERE tenant_id=p_tenant AND asset_id=src.asset_id;
 IF next_no>2147483647 THEN RETURN NULL;END IF;
 INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,parent_asset_version_id,
  created_by_run_id,content_hash,metadata,review_state,version_no)
 VALUES(identity,p_tenant,src.asset_id,selected.image_id,src.id,v.run_id,selected.image_sha,image,'accepted',next_no);
 INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal)
 VALUES(p_tenant,identity,src.id,'reflection_source',0);
 RETURN identity;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.apply_v28_reflection_version(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v28_reflection_version(uuid,uuid,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.apply_v28_reflection_version(uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
