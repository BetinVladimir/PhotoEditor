-- +goose Up
-- Called after canonical task/output locks: these predicates MUST remain read
-- only. Reusing tenant-first admission locks here would invert lock ordering.
-- Frozen closures are immutable; live source, actor, session and independent
-- qualification authority are still required at every execution phase.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_scene_source_pins_live(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_pins jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; a photo.artifacts%ROWTYPE; actual jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR jsonb_typeof(p_pins) IS DISTINCT FROM 'array' OR pg_column_size(p_pins)>4194304 THEN RETURN false; END IF;
 IF jsonb_array_length(p_pins)<2 THEN RETURN false; END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(p_pins) entries(value) WHERE entries.value->>'contract'='WorkingImage')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(p_pins) entries(value) WHERE entries.value->>'contract'='WorkingPixels')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(p_pins) entries(value) WHERE entries.value->>'contract'='WorkingAlpha')>1
 OR p_pins->0->>'contract' IS DISTINCT FROM 'WorkingImage'
 OR p_pins->1->>'contract' IS DISTINCT FROM 'WorkingPixels'
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(p_pins) WITH ORDINALITY entry(pin,ordinal)
 WHERE jsonb_typeof(entry.pin) IS DISTINCT FROM 'object' OR (entry.ordinal>2 AND entry.pin->>'contract' IS DISTINCT FROM 'DenseWarpField' AND NOT(entry.ordinal=3 AND entry.pin->>'contract'='WorkingAlpha'))) THEN RETURN false; END IF;

 FOR pin IN SELECT value FROM jsonb_array_elements(p_pins) LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->>'artifact_id')::uuid;
  IF NOT FOUND THEN RETURN false; END IF;
  IF pin->>'contract' IS NULL OR pin->>'contract' NOT IN('WorkingImage','WorkingPixels','WorkingAlpha','DenseWarpField') THEN RETURN false; END IF;
  IF pin->>'contract'='WorkingImage' THEN
   IF a.id<>p_source OR a.sha256<>p_source_sha OR pin->>'parent_id' IS NOT NULL OR pin->>'parent_sha' IS NOT NULL THEN RETURN false; END IF;
  ELSE
   IF pin->>'parent_id' IS DISTINCT FROM p_source::text OR pin->>'parent_sha' IS DISTINCT FROM p_source_sha THEN RETURN false; END IF;
  END IF;
  -- Admission already froze the full closure. Recheck the accepted master and
  -- exact immutable object tuples without calling scope helpers that may lock.
  IF NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets asset
   ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
   WHERE v.tenant_id=p_tenant AND v.id=p_version AND asset.project_id=p_project
   AND asset.deleted_at IS NULL AND asset.upload_state='verified' AND v.review_state='accepted'
   AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha) THEN RETURN false;END IF;
  IF a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR a.expires_at<=clock_timestamp() THEN RETURN false; END IF;
  actual:=jsonb_build_object('contract',pin->>'contract','artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type,'parent_id',pin->>'parent_id','parent_sha',pin->>'parent_sha');
  IF actual IS DISTINCT FROM pin THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
END $$;

CREATE FUNCTION photo.semantic_scene_qualification_live(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_expected jsonb,p_pin jsonb)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_typeof(p_pin)='object' AND pg_column_size(p_pin)<=16384 AND
 (SELECT count(*)=1 FROM photo.semantic_proposal_scene_qualification_object(p_tenant,p_project,p_actor,p_session,p_expected) item
 WHERE jsonb_build_object('contract','QualificationReport','artifact_id',item.artifact_id,'sha256',item.sha256,
 'bucket',item.bucket,'key',item.object_key,'version',item.object_version,'bytes',item.bytes,'backend','cuda')=p_pin),false);
$$;
CREATE FUNCTION photo.semantic_scene_params_live(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_document uuid,p_params uuid,p_params_sha text,p_pin jsonb)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_typeof(p_pin)='object' AND pg_column_size(p_pin)<=16384 AND
 (SELECT count(*)=1 FROM photo.semantic_defect_version_params_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_document,p_params,p_params_sha) item
 WHERE p_pin=jsonb_build_object('bucket',item.bucket,'key',item.object_key,'version',item.object_version,'sha256',item.sha256,'bytes',item.bytes,'media_type',item.media_type)),false);
$$;
CREATE FUNCTION photo.semantic_scene_scope_live(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; parent_row photo.runs%ROWTYPE; detector photo.runs%ROWTYPE; t photo.tasks%ROWTYPE;
 c jsonb; n jsonb; manifest jsonb; frozen jsonb; project uuid; actor uuid; session_id uuid; source_pins jsonb;
 entry jsonb; params_pin jsonb; params_count integer:=0;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT config_snapshot->'v27_scene_prepare' INTO c FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF photo.semantic_scene_marker_identity(p_run,c) IS NOT TRUE OR c->>'task_id' IS DISTINCT FROM p_task::text THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active';IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO parent_row FROM photo.runs WHERE tenant_id=p_tenant AND id=(c->>'parent_run_id')::uuid;
 SELECT * INTO detector FROM photo.runs WHERE tenant_id=p_tenant AND id=photo.semantic_uuid_v5((c->>'parent_run_id')::uuid,'semantic.face.detect:reference');
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task;
 IF r.id IS NULL OR detector.id IS NULL OR t.id IS NULL
 OR r.config_snapshot->'v27_scene_prepare' IS DISTINCT FROM c
 OR r.kind<>'v2' OR r.cancel_requested OR r.status NOT IN('draft','queued','running','waiting_review','completed')
 -- The original parent is admitted only after these prerequisites finish.
 -- An existing row must match; absence is anchored by the sealed detector.
 OR (parent_row.id IS NOT NULL AND (parent_row.kind<>'v2' OR parent_row.created_by IS DISTINCT FROM r.created_by
 OR parent_row.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR parent_row.cancel_requested OR parent_row.status NOT IN('draft','queued','running','waiting_review')))
 OR detector.kind<>'v2' OR NOT detector.graph_sealed OR detector.cancel_requested
 OR detector.status NOT IN('queued','running','completed') OR detector.v27_initial_manual_selection IS NOT NULL
 OR r.created_by IS DISTINCT FROM detector.created_by OR r.input_snapshot_id IS DISTINCT FROM detector.input_snapshot_id
 OR t.cancel_requested OR t.operation IS DISTINCT FROM c->>'operation' OR t.operation_version<>'1'
 OR t.resource_class<>'gpu' OR t.node_key IS DISTINCT FROM (c->>'operation')||':0'
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1 THEN RETURN false;END IF;
 n:=detector.config_snapshot->'v27_detector_prepare';project:=(r.config_snapshot->>'project_id')::uuid;actor:=r.created_by;session_id:=(c->>'auth_session_id')::uuid;
 IF n->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR n->'parent_run_id' IS DISTINCT FROM c->'parent_run_id' OR n->'request_sha' IS DISTINCT FROM c->'request_sha'
 OR n->'auth_session_id' IS DISTINCT FROM c->'auth_session_id'
 OR detector.config_snapshot->>'project_id' IS DISTINCT FROM project::text
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=detector.id
  AND w.id=photo.semantic_uuid_v5(detector.id,'workflow') AND w.project_id=project
  AND w.input_snapshot_id=r.input_snapshot_id AND w.workflow_type='semantic' AND w.renderer_version='v2.2')
 OR (parent_row.id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=parent_row.id
  AND w.project_id=project AND w.input_snapshot_id=r.input_snapshot_id AND w.workflow_type='semantic'))
 OR photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS NOT TRUE THEN RETURN false;END IF;
 IF c->'source' IS DISTINCT FROM n->'source' THEN
  IF detector.v27_target_manifest IS NULL OR detector.v27_target_manifest_sha IS NULL
  OR encode(public.digest(convert_to(detector.v27_target_manifest,'UTF8'),'sha256'),'hex') IS DISTINCT FROM detector.v27_target_manifest_sha THEN RETURN false;END IF;
  manifest:=detector.v27_target_manifest::jsonb;
  IF (manifest-'entries') IS DISTINCT FROM jsonb_build_object('schema_version',1,'tenant_id',p_tenant,'project_id',project,'actor_id',actor,
   'session_id',session_id,'parent_run_id',(c->>'parent_run_id')::uuid,'request_sha',n->'request_sha','intent',n->'intent','snapshot',n->'snapshot')
  OR jsonb_typeof(manifest->'entries') IS DISTINCT FROM 'array'
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(manifest->'entries') e WHERE e->'binding'=c->'source') THEN RETURN false;END IF;
 END IF;
 frozen:=c->'task';
 IF frozen->>'id' IS DISTINCT FROM t.id::text OR frozen->>'input_digest' IS DISTINCT FROM t.input_digest
 OR frozen->>'params_artifact_id' IS DISTINCT FROM t.params_artifact_id::text
 OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM frozen->'execution_profile'
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cuda'
 OR t.execution_profile->>'model_lock_sha' IS DISTINCT FROM c->'qualification_expected'->>'ModelLockSHA'
 OR t.execution_profile->>'runtime_lock_sha' IS DISTINCT FROM c->'qualification_expected'->>'RuntimeLockSHA'
 OR t.execution_profile->>'device_profile_sha' IS DISTINCT FROM c->'qualification_expected'->>'DeviceProfileSHA'
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.project_id=project
  AND w.id=photo.semantic_uuid_v5(p_run,'workflow') AND w.input_snapshot_id=r.input_snapshot_id AND w.workflow_type='semantic'
  AND w.policy_sha=encode(public.digest(convert_to(photo.v27_target_manifest_json_text(c->'qualification_expected'),'UTF8'),'sha256'),'hex')) THEN RETURN false;END IF;
 -- Registration and physical locators remain exactly the frozen closure.
 IF photo.v2_input_closure_ready(p_task) IS NOT TRUE
 OR (SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.ordinal),'[]'::jsonb)
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal<1000000)
 IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_object('ref',e->'Ref','contract',CASE WHEN e->>'Contract' IN('WorkingPixels','WorkingAlpha','DenseWarpField') THEN 'NpyArray' ELSE e->>'Contract' END)
 ORDER BY (e->'Ref'->>'artifact_id')::uuid,e->'Ref'->>'sha256',e->>'Contract') FROM jsonb_array_elements(c->'input_objects') e)
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(c->'input_objects') e WHERE NOT EXISTS(
 SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(e->'Ref'->>'artifact_id')::uuid
 AND a.sha256=e->'Ref'->>'sha256' AND a.sha256=e->'Object'->>'SHA256'
 AND a.bucket=c->'qualification_report'->>'bucket' AND a.object_key=e->'Object'->>'Key'
 AND a.object_version=e->'Object'->>'VersionID' AND a.object_version NOT IN('','null')
 AND a.bytes=(e->'Object'->>'Bytes')::bigint AND a.media_type=e->'Object'->>'ContentType'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN false;END IF;
 -- Reconstruct the same source-pin format used at original admission, retaining
 -- the frozen bucket rather than selecting a newly configured storage location.
 SELECT jsonb_agg(jsonb_build_object('contract',e->'Node'->'Contract','artifact_id',e->'Node'->'Ref'->'artifact_id','sha256',e->'Node'->'Ref'->'sha256',
  'bucket',c->'qualification_report'->'bucket','key',e->'Object'->'Key','version',e->'Object'->'VersionID','bytes',e->'Object'->'Bytes','media_type',e->'Object'->'ContentType',
  'parent_id',CASE WHEN e->'Node'->'ParentRef'->>'artifact_id'='00000000-0000-0000-0000-000000000000' THEN 'null'::jsonb ELSE e->'Node'->'ParentRef'->'artifact_id' END,
  'parent_sha',CASE WHEN e->'Node'->'ParentRef'->>'artifact_id'='00000000-0000-0000-0000-000000000000' THEN 'null'::jsonb ELSE e->'Node'->'ParentRef'->'sha256' END) ORDER BY ordinal)
 INTO source_pins FROM jsonb_array_elements(c->'source_objects') WITH ORDINALITY x(e,ordinal);
 IF photo.semantic_scene_source_pins_live(p_tenant,project,actor,session_id,(c->'source'->>'asset_version_id')::uuid,
  (c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256',source_pins) IS NOT TRUE
 OR photo.semantic_scene_qualification_live(p_tenant,project,actor,session_id,c->'qualification_expected',c->'qualification_report') IS NOT TRUE THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(c->'input_objects') LOOP
  IF entry->>'Contract'='Params' THEN
   params_count:=params_count+1;
   IF entry->'Ref' IS DISTINCT FROM c->'params' THEN RETURN false;END IF;
   params_pin:=jsonb_build_object('bucket',c->'qualification_report'->'bucket','key',entry->'Object'->'Key','version',entry->'Object'->'VersionID','sha256',entry->'Object'->'SHA256','bytes',entry->'Object'->'Bytes','media_type',entry->'Object'->'ContentType');
  END IF;
 END LOOP;
 IF params_count<>1 OR photo.semantic_scene_params_live(p_tenant,project,actor,session_id,(c->'source'->>'asset_version_id')::uuid,
  (c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256',(c->>'params_document')::uuid,
  (c->'params'->>'artifact_id')::uuid,c->'params'->>'sha256',params_pin) IS NOT TRUE THEN RETURN false;END IF;
 -- Re-read expiring source/report authority last; this function acquires no locks.
 IF photo.semantic_scene_source_pins_live(p_tenant,project,actor,session_id,(c->'source'->>'asset_version_id')::uuid,
  (c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256',source_pins) IS NOT TRUE
 OR photo.semantic_scene_qualification_live(p_tenant,project,actor,session_id,c->'qualification_expected',c->'qualification_report') IS NOT TRUE
 OR photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS NOT TRUE THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;

CREATE FUNCTION photo.semantic_scene_task_live(p_task uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF NOT (r.config_snapshot ? 'v27_scene_prepare') THEN RETURN true;END IF;
 RETURN t.operation IN('scene.parse','depth.predict') AND EXISTS(SELECT 1 FROM photo.operation_registry op
 WHERE op.operation=t.operation AND op.version=t.operation_version AND op.v2_enabled
 AND op.resource_class=t.resource_class AND op.backends ? (t.execution_profile->>'backend'))
 AND photo.semantic_scene_scope_live(t.tenant_id,t.run_id,t.id) IS TRUE;
END $$;
-- Preserve execution_source_live's identity and the entire installed predicate.
DO $patch$
DECLARE body text;needle text:=' AND photo.semantic_shown_exposure_task_live(p_task) IS TRUE';target regprocedure:='photo.execution_source_live(uuid)'::regprocedure;
BEGIN
 SELECT prosrc INTO body FROM pg_proc WHERE oid=target;
 IF (SELECT pg_get_userbyid(proowner) FROM pg_proc WHERE oid=target)<>'photo_fn_owner'
 OR length(body)-length(replace(body,needle,''))<>length(needle)
 OR position('photo.semantic_scene_task_live' in body)>0 THEN RAISE EXCEPTION 'SCENE_EXECUTION_SOURCE_INSTALLATION_DRIFT';END IF;
 body:=replace(body,needle,' AND photo.semantic_scene_task_live(p_task) IS TRUE'||needle);
 EXECUTE 'CREATE OR REPLACE FUNCTION photo.execution_source_live(p_task uuid) RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS '||quote_literal(body);
END $patch$;
DO $acl$
DECLARE f record;entry record;
BEGIN
 FOR f IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND p.proname IN('semantic_scene_source_pins_live','semantic_scene_qualification_live','semantic_scene_params_live','semantic_scene_scope_live','semantic_scene_task_live') LOOP
  EXECUTE format('ALTER FUNCTION %s OWNER TO photo_fn_owner',f.oid::regprocedure);
  EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC',f.oid::regprocedure);
  FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
   WHERE p.oid=f.oid AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',f.oid::regprocedure,(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
  END LOOP;
 END LOOP;
END $acl$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
