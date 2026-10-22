-- +goose Up
-- Scene metadata is not authority: every final graph retains canonical producer
-- and live qualification checks before activation and during later task use.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_native_scene_graph_ready(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;marker jsonb;scenes jsonb;nodes jsonb;pin jsonb;policy photo.v2_qualification_policies%ROWTYPE;
 scene jsonb;node jsonb;target jsonb;t photo.tasks%ROWTYPE;idx integer:=0;record_value jsonb;assembly_id uuid;h bytea;workflow_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF r.id IS NULL THEN RETURN false;END IF;
 marker:=r.config_snapshot->'v27_native_targets_transfer';scenes:=marker->'scene_evidence';
 IF scenes IS NULL THEN
  RETURN NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND execution_profile ? 'semantic_scene');
 END IF;
 nodes:=r.config_snapshot->'v27_semantic'->'nodes';pin:=r.config_snapshot->'v27_semantic'->'qualification';
 IF jsonb_typeof(marker) IS DISTINCT FROM 'object' OR jsonb_typeof(scenes) IS DISTINCT FROM 'array'
 OR jsonb_typeof(nodes) IS DISTINCT FROM 'array' OR jsonb_typeof(marker->'targets') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(scenes) NOT BETWEEN 1 AND 5000 OR jsonb_array_length(scenes)<>jsonb_array_length(nodes)
 OR jsonb_array_length(scenes)<>jsonb_array_length(marker->'targets')
 OR marker->>'parent_run_id' IS DISTINCT FROM p_run::text
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='semantic.transfer')<>jsonb_array_length(nodes) THEN RETURN false;END IF;
 -- Assembly is an existing later stage in this same run. Permit only its
 -- registered deterministic identity, never an arbitrary additional task.
 SELECT id INTO workflow_id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF workflow_id IS NULL THEN RETURN false;END IF;
 h:=public.digest(uuid_send(workflow_id)||convert_to('v2.semantic.assemble:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 assembly_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation<>'semantic.transfer'
 AND NOT(id=assembly_id AND node_key='v2.semantic.assemble:0' AND operation='v2.semantic.assemble'
 AND operation_version='1' AND resource_class='cpu' AND execution_profile->>'backend'='cpu')) THEN RETURN false;END IF;
 SELECT * INTO policy FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid FOR SHARE;
 IF policy.id IS NULL OR policy.operation<>'semantic.transfer' OR policy.operation_version<>'1' OR NOT policy.enabled OR NOT policy.admission_enabled THEN RETURN false;END IF;
 record_value:=jsonb_build_object('pin',pin,'policy',jsonb_build_object('required_checks',policy.required_checks,'evidence_shas',policy.evidence_shas));
 FOR scene IN SELECT value FROM jsonb_array_elements(scenes) LOOP
  node:=nodes->idx;target:=marker->'targets'->idx;
  IF jsonb_typeof(scene) IS DISTINCT FROM 'object' OR scene->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR scene->'reference'->'binding' IS DISTINCT FROM marker->'reference'
  OR scene->'target'->'binding' IS DISTINCT FROM target->'source'
  OR scene->'intent' IS DISTINCT FROM marker->'intent' OR scene->'calibration' IS DISTINCT FROM pin->'calibration'
  OR target->'ordinal' IS DISTINCT FROM to_jsonb(idx)
  OR photo.v27_semantic_scene_policy_ready(scene,record_value) IS DISTINCT FROM true THEN RETURN false;END IF;
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(node->>'task_id')::uuid FOR SHARE;
  IF t.id IS NULL OR t.operation<>'semantic.transfer' OR t.operation_version<>'1' OR t.execution_profile->'semantic_scene' IS DISTINCT FROM scene
  OR photo.v27_semantic_scene_origin(p_tenant,t.id,scene) IS DISTINCT FROM true THEN RETURN false;END IF;
  idx:=idx+1;
 END LOOP;
 -- A later ordinal can wait on producer locks long enough for an earlier
 -- source to expire. Repeat the entire origin proof after all locks are held.
 idx:=0;
 FOR scene IN SELECT value FROM jsonb_array_elements(scenes) LOOP
  node:=nodes->idx;
  IF photo.v27_semantic_scene_origin(p_tenant,(node->>'task_id')::uuid,scene) IS DISTINCT FROM true THEN RETURN false;END IF;
  idx:=idx+1;
 END LOOP;
 -- Policy withdrawal while waiting on source/producer locks must win.
 SELECT * INTO policy FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid;
 RETURN policy.id IS NOT NULL AND policy.enabled AND policy.admission_enabled;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v27_native_scene_graph_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_native_scene_graph_ready(uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
DO $wrap$
DECLARE oid_value oid;item record;name text;
BEGIN
 FOREACH name IN ARRAY ARRAY['seal_v2_graph','v27_semantic_frozen_qualification_ready'] LOOP
  oid_value:=to_regprocedure('photo.'||name||'(uuid,uuid)');
  IF oid_value IS NULL OR (SELECT pg_get_userbyid(proowner) FROM pg_proc WHERE oid=oid_value)<>'photo_fn_owner'
  OR to_regprocedure('photo.'||name||'_pre_frozen_scene(uuid,uuid)') IS NOT NULL THEN RAISE EXCEPTION 'SEMANTIC_SCENE_HANDOFF_INSTALLATION_DRIFT';END IF;
  EXECUTE format('ALTER FUNCTION photo.%I(uuid,uuid) RENAME TO %I',name,name||'_pre_frozen_scene');
  EXECUTE format('REVOKE ALL ON FUNCTION photo.%I(uuid,uuid) FROM PUBLIC',name||'_pre_frozen_scene');
  FOR item IN SELECT a.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
   WHERE p.oid=oid_value AND a.grantee<>0 AND a.grantee<>p.proowner LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION photo.%I(uuid,uuid) FROM %I',name||'_pre_frozen_scene',(SELECT rolname FROM pg_roles WHERE oid=item.grantee));
  END LOOP;
 END LOOP;
END $wrap$;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v27_native_scene_graph_ready(p_tenant,p_run) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_frozen_scene(p_tenant,p_run);
END $$;
CREATE FUNCTION photo.v27_semantic_frozen_qualification_ready(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v27_semantic_frozen_qualification_ready_pre_frozen_scene(p_tenant,p_run) IS DISTINCT FROM true
 OR photo.v27_native_scene_graph_ready(p_tenant,p_run) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.v27_semantic_frozen_qualification_ready_pre_frozen_scene(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v27_semantic_frozen_qualification_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid),photo.v27_semantic_frozen_qualification_ready(uuid,uuid) FROM PUBLIC;
DO $acl$ DECLARE item record;BEGIN
 FOR item IN SELECT p.oid::regprocedure signature,a.grantee FROM pg_proc p
 CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE p.oid IN('photo.v27_native_scene_graph_ready(uuid,uuid)'::regprocedure,'photo.seal_v2_graph(uuid,uuid)'::regprocedure,'photo.v27_semantic_frozen_qualification_ready(uuid,uuid)'::regprocedure)
 AND a.grantee<>0 AND a.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',item.signature,(SELECT rolname FROM pg_roles WHERE oid=item.grantee));
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid),photo.v27_semantic_frozen_qualification_ready(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
