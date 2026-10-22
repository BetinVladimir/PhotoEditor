-- +goose Up
-- Embedded immutable scene evidence. This does not activate an operation or
-- manufacture qualification; actual physical evidence and source decoding are
-- independently verified by worker/adopter.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_scene_origin(p_tenant uuid,p_task uuid,p_scene jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid; source jsonb; operation text; root jsonb; params jsonb; child jsonb;
 producer photo.tasks%ROWTYPE; run_row photo.runs%ROWTYPE; result photo.task_results%ROWTYPE;
 source_id uuid; source_sha text; version_id uuid; root_id uuid; root_sha text;
 params_id uuid; params_sha text; child_id uuid; child_sha text; expected_role text; expected_contract text; binary_role text; invalid jsonb;
BEGIN
 IF p_scene IS NULL OR jsonb_typeof(p_scene)<>'object' OR p_scene->>'schema_version' IS DISTINCT FROM '1'
 OR p_scene->'policy'->>'algorithm' IS DISTINCT FROM 'farneback_depth_plane_v1' THEN RETURN false;END IF;
 SELECT w.project_id INTO project FROM photo.tasks t JOIN photo.v2_workflows w
 ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='semantic.transfer' AND t.operation_version='1';
 IF project IS NULL THEN RETURN false;END IF;
 -- Acquire cross-producer ownership before any artifact/version locks. Roots'
 -- created_by_task_id is immutable metadata; lifecycle state is rechecked below.
 -- Sorted whole-set acquisition avoids reference/target reverse-order deadlocks.
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id IN(
  SELECT t.run_id FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
  WHERE t.tenant_id=p_tenant AND a.id IN(
   SELECT (entry->>'artifact_id')::uuid FROM jsonb_array_elements(jsonb_build_array(
    p_scene->'reference'->'labels',p_scene->'reference'->'depth',p_scene->'target'->'labels',p_scene->'target'->'depth')) entry))
 ORDER BY r.id FOR SHARE OF r;
 PERFORM t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id IN(
  SELECT a.created_by_task_id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(
   SELECT (entry->>'artifact_id')::uuid FROM jsonb_array_elements(jsonb_build_array(
    p_scene->'reference'->'labels',p_scene->'reference'->'depth',p_scene->'target'->'labels',p_scene->'target'->'depth')) entry))
 ORDER BY t.id FOR SHARE OF t;
 PERFORM result_row.id FROM photo.task_results result_row JOIN photo.tasks t
 ON t.tenant_id=result_row.tenant_id AND t.canonical_result_id=result_row.id
 WHERE t.tenant_id=p_tenant AND t.id IN(
  SELECT a.created_by_task_id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(
   SELECT (entry->>'artifact_id')::uuid FROM jsonb_array_elements(jsonb_build_array(
    p_scene->'reference'->'labels',p_scene->'reference'->'depth',p_scene->'target'->'labels',p_scene->'target'->'depth')) entry))
 ORDER BY result_row.id FOR SHARE OF result_row;
 PERFORM a.id FROM photo.assets a JOIN photo.asset_versions v ON v.tenant_id=a.tenant_id AND v.asset_id=a.id
 WHERE v.tenant_id=p_tenant AND v.id IN(
  (p_scene->'reference'->'binding'->>'asset_version_id')::uuid,(p_scene->'target'->'binding'->>'asset_version_id')::uuid)
 ORDER BY a.id FOR SHARE OF a;
 PERFORM v.id FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND v.id IN(
  (p_scene->'reference'->'binding'->>'asset_version_id')::uuid,(p_scene->'target'->'binding'->>'asset_version_id')::uuid)
 ORDER BY v.id FOR SHARE OF v;
 PERFORM a.id FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task ORDER BY a.id FOR SHARE OF a;
 FOR source IN SELECT value FROM jsonb_array_elements(jsonb_build_array(p_scene->'reference',p_scene->'target')) LOOP
  version_id:=(source->'binding'->>'asset_version_id')::uuid;
  source_id:=(source->'binding'->'source'->>'artifact_id')::uuid;
  source_sha:=source->'binding'->'source'->>'sha256';
  PERFORM v.id FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND v.id=version_id FOR SHARE;
  PERFORM a.id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   WHERE v.tenant_id=p_tenant AND v.id=version_id FOR SHARE OF a;
  IF source_id IS NULL OR source_sha IS NULL OR source_sha !~ '^[0-9a-f]{64}$'
  OR NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   WHERE v.tenant_id=p_tenant AND v.id=version_id AND v.master_artifact_id=source_id AND v.content_hash=source_sha
   AND v.review_state='accepted' AND a.project_id=project AND a.deleted_at IS NULL AND a.upload_state='verified')
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.project_id=project AND i.artifact_id=source_id AND i.sha256=source_sha AND i.contract='WorkingImage') THEN RETURN false;END IF;
  FOREACH operation IN ARRAY ARRAY['scene.parse','depth.predict'] LOOP
   IF operation='scene.parse' THEN root:=source->'labels';params:=source->'labels_params';child:=source->'labels_pixels';expected_role:='scene_labels_ref';expected_contract:='SceneLabels';binary_role:='scene_labels';
   ELSE root:=source->'depth';params:=source->'depth_params';child:=source->'depth_pixels';expected_role:='relative_depth_ref';expected_contract:='RelativeDepth';binary_role:='relative_depth';END IF;
   root_id:=(root->>'artifact_id')::uuid;root_sha:=root->>'sha256';params_id:=(params->>'artifact_id')::uuid;params_sha:=params->>'sha256';child_id:=(child->>'artifact_id')::uuid;child_sha:=child->>'sha256';
   IF root_id IS NULL OR params_id IS NULL OR child_id IS NULL OR root_sha IS NULL OR params_sha IS NULL OR child_sha IS NULL
   OR root_sha !~ '^[0-9a-f]{64}$' OR params_sha !~ '^[0-9a-f]{64}$' OR child_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
   SELECT t.* INTO producer FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
   WHERE a.tenant_id=p_tenant AND a.id=root_id AND a.sha256=root_sha;
   IF producer.id IS NULL OR producer.operation IS DISTINCT FROM operation OR producer.operation_version<>'1'
   OR producer.state<>'succeeded' OR producer.cancel_requested OR producer.attempt<1 OR producer.fencing_token<1
   OR producer.params_artifact_id IS DISTINCT FROM params_id THEN RETURN false;END IF;
   SELECT * INTO run_row FROM photo.runs WHERE tenant_id=p_tenant AND id=producer.run_id FOR SHARE;
   SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer.id FOR SHARE;
   SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id
    AND task_id=producer.id AND attempt=producer.attempt AND fencing_token=producer.fencing_token FOR SHARE;
   IF run_row.id IS NULL OR run_row.cancel_requested OR run_row.status NOT IN('running','waiting_review','completed') OR result.id IS NULL
   OR (run_row.config_snapshot->>'project_id'=project::text OR EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=run_row.id AND w.project_id=project)) IS DISTINCT FROM TRUE
   OR jsonb_typeof(result.outputs) IS DISTINCT FROM 'array' OR (result.outputs @> jsonb_build_array(root,child)) IS DISTINCT FROM TRUE
   OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=root_id AND a.sha256=root_sha
    AND a.output_role=expected_role AND a.media_type='application/json' AND a.created_by_task_id=producer.id
    AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token)
   OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=child_id AND a.sha256=child_sha
    AND a.created_by_task_id=producer.id AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token
    AND a.output_role=binary_role AND a.media_type IN('application/octet-stream','application/x-npy'))
   OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=params_id AND a.sha256=params_sha AND a.media_type='application/json')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.project_id=project AND i.artifact_id=root_id AND i.sha256=root_sha AND i.contract=expected_contract)
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.project_id=project AND i.artifact_id=params_id AND i.sha256=params_sha AND i.contract=CASE WHEN operation='scene.parse' THEN 'SceneParseParams' ELSE 'DepthParams' END)
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.project_id=project AND i.artifact_id=child_id AND i.sha256=child_sha AND i.contract='NpyArray')
   OR NOT (EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=producer.id AND i.project_id=project AND i.artifact_id=source_id AND i.sha256=source_sha AND i.contract='WorkingImage')
    OR producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',source_id::text,'sha256',source_sha))) THEN RETURN false;END IF;
   IF operation='depth.predict' AND source->'depth_invalid' IS DISTINCT FROM 'null'::jsonb THEN
    invalid:=source->'depth_invalid';
    IF invalid IS NULL OR jsonb_typeof(invalid)<>'object'
    OR (result.outputs @> jsonb_build_array(invalid)) IS DISTINCT FROM TRUE
    OR NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.v2_task_inputs i
     ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
     WHERE a.tenant_id=p_tenant AND a.id=(invalid->>'artifact_id')::uuid AND a.sha256=invalid->>'sha256'
     AND a.output_role='depth_invalid' AND a.created_by_task_id=producer.id AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token
     AND a.media_type IN('application/octet-stream','application/x-npy') AND i.task_id=p_task AND i.project_id=project AND i.contract='NpyArray') THEN RETURN false;END IF;
   END IF;
   -- Every declared scene child must still be physically readable; full document
   -- hashes and exact Params.image are checked independently outside SQL.
   IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
    WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes<1
    OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v27_semantic_scene_origin(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_scene_origin(uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_scene_policy_ready(p_scene jsonb,p_record jsonb)
RETURNS boolean LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_typeof(p_scene)='object' AND jsonb_typeof(p_record)='object'
 AND (p_record->'policy'->'required_checks') @> '["semantic.scene.farneback_depth_plane_v1"]'::jsonb
 AND (p_record->'policy'->'evidence_shas') @> jsonb_build_array(p_scene->>'policy_sha')
 AND p_scene->>'model_lock_sha'=p_record->'pin'->>'model_lock_sha'
 AND p_scene->>'runtime_lock_sha'=p_record->'pin'->>'runtime_lock_sha'
 AND p_scene->>'device_profile_sha'=p_record->'pin'->>'device_profile_sha'
 AND p_scene->>'dataset_sha'=p_record->'pin'->>'dataset_sha',false);
$$;
ALTER FUNCTION photo.v27_semantic_scene_policy_ready(jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_scene_policy_ready(jsonb,jsonb) FROM PUBLIC;

CREATE FUNCTION photo.v27_semantic_worker_scene(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scene jsonb; evidence jsonb;
BEGIN
 evidence:=photo.v27_semantic_worker_report_evidence(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_digest);
 IF evidence IS NULL THEN RETURN NULL;END IF;
 SELECT execution_profile->'semantic_scene' INTO scene FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run;
 IF photo.v27_semantic_scene_policy_ready(scene,evidence) IS DISTINCT FROM TRUE
 OR photo.v27_semantic_scene_origin(p_tenant,p_task,scene) IS DISTINCT FROM TRUE THEN RETURN NULL;END IF;
 RETURN scene;
END $$;
ALTER FUNCTION photo.v27_semantic_worker_scene(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_worker_scene(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_worker_scene(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_worker;

CREATE FUNCTION photo.v27_semantic_adoption_scene(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scene jsonb; evidence jsonb;
BEGIN
 evidence:=photo.v27_semantic_adoption_report_evidence(p_tenant,p_task,p_attempt,p_fence,p_result);
 IF evidence IS NULL THEN RETURN NULL;END IF;
 SELECT execution_profile->'semantic_scene' INTO scene FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF photo.v27_semantic_scene_policy_ready(scene,evidence) IS DISTINCT FROM TRUE
 OR photo.v27_semantic_scene_origin(p_tenant,p_task,scene) IS DISTINCT FROM TRUE THEN RETURN NULL;END IF;
 RETURN scene;
END $$;
ALTER FUNCTION photo.v27_semantic_adoption_scene(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_adoption_scene(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_adoption_scene(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- Remove explicit default ACLs too; nested origin/policy helpers stay owner-only.
DO $acl$ DECLARE f record; grant_row record; BEGIN
 FOR f IN SELECT p.oid,p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND p.proname IN('v27_semantic_scene_origin','v27_semantic_scene_policy_ready','v27_semantic_worker_scene','v27_semantic_adoption_scene') LOOP
  FOR grant_row IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
   WHERE p.oid=f.oid AND x.grantee<>p.proowner AND x.grantee<>0 LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',f.oid::regprocedure,(SELECT rolname FROM pg_roles WHERE oid=grant_row.grantee));
  END LOOP;
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_worker_scene(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_worker;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_adoption_scene(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- Producer qualification is separate from the final transfer's qualification.
-- No scene/depth model report is inferred from the transfer report's hash.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_scene_qualification_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_expected jsonb)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT report.id,report.sha256,report.bucket,report.object_key,report.object_version,report.bytes
 FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project
 AND q.operation=policy.operation AND q.operation_version=policy.operation_version
 AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
 AND q.backend=policy.backend AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND policy.operation IN('scene.parse','depth.predict') AND policy.operation=p_expected->>'Operation'
 AND policy.operation_version='1' AND p_expected->>'Version'='1' AND policy.backend='cuda'
 AND policy.enabled AND policy.admission_enabled AND policy.license_cleared
 AND policy.model_lock_sha=p_expected->>'ModelLockSHA' AND policy.runtime_lock_sha=p_expected->>'RuntimeLockSHA'
 AND policy.backend=p_expected->>'Backend' AND policy.device_profile_sha=p_expected->>'DeviceProfileSHA'
 AND policy.dataset_sha=p_expected->>'DatasetSHA' AND policy.issuer_id::text=p_expected->>'IssuerID'
 AND (p_expected->>'LicenseCleared')::boolean
 AND to_jsonb(ARRAY(SELECT unnest(policy.required_checks) ORDER BY 1))=(SELECT jsonb_agg(value ORDER BY value) FROM jsonb_array_elements_text(p_expected->'RequiredChecks'))
 AND q.trusted_import AND q.status='passed'
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=p_tenant AND log.action='v2_qualification.imported'
 AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=policy.id::text)
 AND report.state='ready' AND report.verified_at IS NOT NULL AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
 AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 ORDER BY report.id;
$$;
ALTER FUNCTION photo.semantic_proposal_scene_qualification_object(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_scene_qualification_object(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
DO $acl$ DECLARE entry record; BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.semantic_proposal_scene_qualification_object(uuid,uuid,uuid,uuid,jsonb)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.semantic_proposal_scene_qualification_object(uuid,uuid,uuid,uuid,jsonb) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_scene_qualification_object(uuid,uuid,uuid,uuid,jsonb) TO photo_api;
CREATE FUNCTION photo.lock_semantic_proposal_scene_qualification(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_expected jsonb,p_pin jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item record; matches integer:=0;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true
 OR jsonb_typeof(p_pin) IS DISTINCT FROM 'object' OR pg_column_size(p_pin)>16384 THEN RETURN false;END IF;
 -- Lock selected policy/import/report as a whole, in stable ID order; repeat
 -- the same scoped catalog predicate after waiting for competing withdrawal.
 PERFORM policy.id FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project
 AND q.operation=policy.operation AND q.operation_version=policy.operation_version
 AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
 AND q.backend=policy.backend AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 WHERE policy.operation IN('scene.parse','depth.predict') AND policy.operation=p_expected->>'Operation'
 AND report.id=(p_pin->>'artifact_id')::uuid AND report.sha256=p_pin->>'sha256'
 ORDER BY policy.id,q.id,report.id FOR SHARE OF policy,q,report;
 FOR item IN SELECT * FROM photo.semantic_proposal_scene_qualification_object(p_tenant,p_project,p_actor,p_session,p_expected) LOOP
  IF jsonb_build_object('contract','QualificationReport','artifact_id',item.artifact_id,'sha256',item.sha256,
   'bucket',item.bucket,'key',item.object_key,'version',item.object_version,'bytes',item.bytes,'backend','cuda')=p_pin THEN
   matches:=matches+1;
  END IF;
 END LOOP;
 RETURN matches=1;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_scene_qualification(uuid,uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_scene_qualification(uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
DO $acl$ DECLARE entry record; BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.lock_semantic_proposal_scene_qualification(uuid,uuid,uuid,uuid,jsonb,jsonb)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_scene_qualification(uuid,uuid,uuid,uuid,jsonb,jsonb) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_scene_qualification(uuid,uuid,uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- Repeat new embedded provenance in the committing adoption transaction. Old
-- immutable tasks without this additive profile keep their original dispatch.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_semantic_scene;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_semantic_scene(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
DO $acl$ DECLARE entry record; BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.adopt_v2_plan_pre_semantic_scene(uuid,uuid,integer,bigint,uuid,jsonb,text)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_semantic_scene(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; scene jsonb; accepted boolean;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.operation='semantic.transfer' AND t.execution_profile ? 'semantic_scene' THEN
  PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
  PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
  PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=t.run_id ORDER BY id FOR UPDATE;
  scene:=photo.v27_semantic_adoption_scene(p_tenant,p_task,p_attempt,p_fence,p_result);
  IF scene IS NULL THEN RETURN false;END IF;
  accepted:=photo.adopt_v2_plan_pre_semantic_scene(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
  IF accepted AND (photo.v27_semantic_scene_origin(p_tenant,p_task,scene) IS DISTINCT FROM true
   OR photo.v27_semantic_adoption_qualification_ready(p_tenant,p_task,p_attempt,p_fence,p_result) IS DISTINCT FROM true) THEN
   RAISE EXCEPTION 'SEMANTIC_SCENE_ADOPTION_STALE' USING ERRCODE='23514';
  END IF;
  RETURN accepted;
 END IF;
 RETURN photo.adopt_v2_plan_pre_semantic_scene(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
DO $acl$ DECLARE entry record; BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- Scene-child identity validation only. This private helper grants no admission,
-- catalog, registration, or execution authority; callers must lock those proofs.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_scene_marker_identity(p_run uuid,p_marker jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE binding jsonb; ref jsonb; op text; parent uuid; namespace uuid; digest text; key text;
BEGIN
 IF p_run IS NULL OR p_run='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(p_marker) IS DISTINCT FROM 'object' OR octet_length(p_marker::text)>4194304
 OR p_marker->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_marker-ARRAY['schema_version','operation','parent_run_id','request_sha','auth_session_id','source','source_binding_sha','source_document_sha','params_document','params','task_id','task_sha','task','reserved_bytes','input_digest','qualification_expected','qualification_report','qualified_costs','input_objects','source_objects','work_icc_sha'])<>'{}'::jsonb
 THEN RETURN false;END IF;
 op:=p_marker->>'operation';
 IF op IS NULL OR op NOT IN('scene.parse','depth.predict') THEN RETURN false;END IF;
 FOREACH key IN ARRAY ARRAY['request_sha','source_binding_sha','source_document_sha','task_sha','input_digest','work_icc_sha'] LOOP
  IF jsonb_typeof(p_marker->key) IS DISTINCT FROM 'string' OR coalesce(p_marker->>key,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 FOREACH key IN ARRAY ARRAY['parent_run_id','auth_session_id','params_document','task_id'] LOOP
  IF jsonb_typeof(p_marker->key) IS DISTINCT FROM 'string'
  OR coalesce(p_marker->>key,'') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR (p_marker->>key)::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 END LOOP;
 binding:=p_marker->'source';
 IF jsonb_typeof(binding) IS DISTINCT FROM 'object' OR (binding-ARRAY['asset_version_id','source','canvas_hash'])<>'{}'::jsonb
 OR jsonb_typeof(binding->'asset_version_id') IS DISTINCT FROM 'string'
 OR coalesce(binding->>'asset_version_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR (binding->>'asset_version_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(binding->'canvas_hash') IS DISTINCT FROM 'string' OR coalesce(binding->>'canvas_hash','') !~ '^[0-9a-f]{64}$'
 THEN RETURN false;END IF;
 FOREACH ref IN ARRAY ARRAY[binding->'source',p_marker->'params'] LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-ARRAY['artifact_id','sha256'])<>'{}'::jsonb
  OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR (ref->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string' OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 digest:=encode(public.digest(convert_to(photo.v27_target_manifest_json_text(binding),'UTF8'),'sha256'),'hex');
 parent:=(p_marker->>'parent_run_id')::uuid;
 namespace:=photo.semantic_uuid_v5(parent,'semantic.scene.source:'||digest);
 IF p_marker->>'source_binding_sha' IS DISTINCT FROM digest
 OR p_marker->>'source_document_sha' IS DISTINCT FROM binding->'source'->>'sha256'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'semantic.scene.'||op||'.run')
 OR p_marker->>'task_id' IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(p_run,'workflow'),op||':0')::text
 OR p_marker->>'params_document' IS DISTINCT FROM photo.semantic_uuid_v5(namespace,'semantic.scene.'||op||'.params')::text
 OR jsonb_typeof(p_marker->'qualification_expected') IS DISTINCT FROM 'object'
 OR p_marker->'qualification_expected'->>'Operation' IS DISTINCT FROM op
 OR p_marker->'qualification_expected'->>'Version' IS DISTINCT FROM '1'
 OR p_marker->'qualification_expected'->>'Backend' IS DISTINCT FROM 'cuda'
 OR jsonb_typeof(p_marker->'qualification_report') IS DISTINCT FROM 'object'
 OR p_marker->'qualification_report'->>'contract' IS DISTINCT FROM 'QualificationReport'
 OR p_marker->'qualification_report'->>'backend' IS DISTINCT FROM 'cuda'
 OR jsonb_typeof(p_marker->'task') IS DISTINCT FROM 'object'
 OR p_marker->'task'->>'id' IS DISTINCT FROM p_marker->>'task_id'
 OR p_marker->'task'->>'operation' IS DISTINCT FROM op
 OR p_marker->'task'->>'operation_version' IS DISTINCT FROM '1'
 OR p_marker->'task'->>'node_key' IS DISTINCT FROM op||':0'
 OR p_marker->'task'->>'resource_class' IS DISTINCT FROM 'gpu'
 OR p_marker->'task'->>'params_artifact_id' IS DISTINCT FROM p_marker->'params'->>'artifact_id'
 OR p_marker->'task'->>'params_sha' IS DISTINCT FROM p_marker->'params'->>'sha256'
 OR p_marker->'task'->>'input_digest' IS DISTINCT FROM p_marker->>'input_digest'
 OR encode(public.digest(convert_to(photo.v27_target_manifest_json_text(p_marker->'task'),'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_marker->>'task_sha'
 OR jsonb_typeof(p_marker->'reserved_bytes') IS DISTINCT FROM 'number'
 OR coalesce(p_marker->>'reserved_bytes','') !~ '^[1-9][0-9]*$'
 OR (p_marker->>'reserved_bytes')::bigint NOT BETWEEN 1 AND 9007199254740991
 OR jsonb_typeof(p_marker->'qualified_costs') IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_marker->'input_objects') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_marker->'source_objects') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_scene_marker_identity(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_scene_marker_identity(uuid,jsonb) FROM PUBLIC;
DO $acl$ DECLARE entry record; BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.semantic_scene_marker_identity(uuid,jsonb)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.semantic_scene_marker_identity(uuid,jsonb) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
-- +goose StatementEnd
-- Shared current authority for scene children. Lifecycle-specific callers must
-- additionally require their exact blocked/sealed/completed state.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_scene_scope(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; parent_row photo.runs%ROWTYPE; detector photo.runs%ROWTYPE; t photo.tasks%ROWTYPE;
 c jsonb; n jsonb; manifest jsonb; frozen jsonb; project uuid; actor uuid; session_id uuid; source_pins jsonb;
 entry jsonb; params_pin jsonb; params_count integer:=0;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT config_snapshot->'v27_scene_prepare' INTO c FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF photo.semantic_scene_marker_identity(p_run,c) IS NOT TRUE OR c->>'task_id' IS DISTINCT FROM p_task::text THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO parent_row FROM photo.runs WHERE tenant_id=p_tenant AND id=(c->>'parent_run_id')::uuid FOR SHARE;
 SELECT * INTO detector FROM photo.runs WHERE tenant_id=p_tenant AND id=photo.semantic_uuid_v5((c->>'parent_run_id')::uuid,'semantic.face.detect:reference') FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
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
 -- Reconstruct the same source-pin format used at original admission, retaining
 -- the frozen bucket rather than selecting a newly configured storage location.
 SELECT jsonb_agg(jsonb_build_object('contract',e->'Node'->'Contract','artifact_id',e->'Node'->'Ref'->'artifact_id','sha256',e->'Node'->'Ref'->'sha256',
  'bucket',c->'qualification_report'->'bucket','key',e->'Object'->'Key','version',e->'Object'->'VersionID','bytes',e->'Object'->'Bytes','media_type',e->'Object'->'ContentType',
  'parent_id',CASE WHEN e->'Node'->'ParentRef'->>'artifact_id'='00000000-0000-0000-0000-000000000000' THEN 'null'::jsonb ELSE e->'Node'->'ParentRef'->'artifact_id' END,
  'parent_sha',CASE WHEN e->'Node'->'ParentRef'->>'artifact_id'='00000000-0000-0000-0000-000000000000' THEN 'null'::jsonb ELSE e->'Node'->'ParentRef'->'sha256' END) ORDER BY ordinal)
 INTO source_pins FROM jsonb_array_elements(c->'source_objects') WITH ORDINALITY x(e,ordinal);
 IF photo.lock_semantic_proposal_source_evidence(p_tenant,project,actor,session_id,(c->'source'->>'asset_version_id')::uuid,
  (c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256',source_pins) IS NOT TRUE
 OR photo.lock_semantic_proposal_scene_qualification(p_tenant,project,actor,session_id,c->'qualification_expected',c->'qualification_report') IS NOT TRUE THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(c->'input_objects') LOOP
  IF entry->>'Contract'='Params' THEN
   params_count:=params_count+1;
   IF entry->'Ref' IS DISTINCT FROM c->'params' THEN RETURN false;END IF;
   params_pin:=jsonb_build_object('bucket',c->'qualification_report'->'bucket','key',entry->'Object'->'Key','version',entry->'Object'->'VersionID','sha256',entry->'Object'->'SHA256','bytes',entry->'Object'->'Bytes','media_type',entry->'Object'->'ContentType');
  END IF;
 END LOOP;
 IF params_count<>1 OR photo.lock_semantic_defect_version_params(p_tenant,project,actor,session_id,(c->'source'->>'asset_version_id')::uuid,
  (c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256',(c->>'params_document')::uuid,
  (c->'params'->>'artifact_id')::uuid,c->'params'->>'sha256',params_pin) IS NOT TRUE THEN RETURN false;END IF;
 -- Params acquisition can wait past a source/report expiry. Repeat the
 -- already-held proof set after the last wait; no new lock order is introduced.
 IF photo.lock_semantic_proposal_source_evidence(p_tenant,project,actor,session_id,(c->'source'->>'asset_version_id')::uuid,
  (c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256',source_pins) IS NOT TRUE
 OR photo.lock_semantic_proposal_scene_qualification(p_tenant,project,actor,session_id,c->'qualification_expected',c->'qualification_report') IS NOT TRUE
 OR photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS NOT TRUE THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_scene_scope(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_scene_scope(uuid,uuid,uuid) FROM PUBLIC;
DO $acl$ DECLARE entry record; BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.lock_semantic_scene_scope(uuid,uuid,uuid)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.lock_semantic_scene_scope(uuid,uuid,uuid) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
-- +goose StatementEnd
-- Exact immutable scene registration and replay; partial state is refused.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_scene_preparation_registration_state(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_params_sha text,p_digest text,p_profile jsonb,p_rows jsonb,p_manifest jsonb,p_chunks jsonb,p_direct jsonb,p_count integer,p_entries_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE; actual jsonb; transport jsonb; expected_transport jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN 'conflict';END IF;
 IF photo.lock_semantic_scene_scope(p_tenant,p_run,p_task) IS NOT TRUE THEN RETURN 'conflict';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('draft','queued','running','completed') OR r.cancel_requested
 OR t.cancel_requested OR t.operation NOT IN('scene.parse','depth.predict') OR t.resource_class<>'gpu' OR t.operation_version<>'1'
 OR jsonb_typeof(r.config_snapshot->'v27_scene_prepare') IS DISTINCT FROM 'object'
 OR r.config_snapshot->'v27_scene_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR r.config_snapshot->'v27_scene_prepare'->>'operation' IS DISTINCT FROM t.operation
 OR r.config_snapshot->'v27_scene_prepare'->>'task_id' IS DISTINCT FROM p_task::text
 OR r.config_snapshot->'v27_scene_prepare'->>'input_digest' IS DISTINCT FROM p_digest
 OR r.config_snapshot->'v27_scene_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)
 OR jsonb_typeof(p_direct) IS DISTINCT FROM 'array' OR jsonb_array_length(p_direct)<>3
 OR NOT (p_direct @> jsonb_build_array(jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)))
 OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_params_sha AND state='ready')
 OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array' OR jsonb_array_length(p_rows)<>p_count
 OR p_count NOT BETWEEN 3 AND 4097 OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array' THEN RETURN 'conflict';END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'v27_scene_prepare'->'source_objects') e WHERE e->'Node'->>'Contract'='WorkingPixels')<>1
 OR p_direct IS DISTINCT FROM (
  SELECT jsonb_agg(ref ORDER BY (ref->>'artifact_id')::uuid) FROM (
   SELECT r.config_snapshot->'v27_scene_prepare'->'params' AS ref
   UNION ALL SELECT r.config_snapshot->'v27_scene_prepare'->'source'->'source'
   UNION ALL SELECT e->'Node'->'Ref' FROM jsonb_array_elements(r.config_snapshot->'v27_scene_prepare'->'source_objects') e WHERE e->'Node'->>'Contract'='WorkingPixels'
  ) roots) THEN RETURN 'conflict';END IF;
 IF p_profile IS DISTINCT FROM r.config_snapshot->'v27_scene_prepare'->'task'->'execution_profile'
 OR p_rows IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_object('ref',e->'Ref','contract',CASE WHEN e->>'Contract' IN('WorkingPixels','WorkingAlpha','DenseWarpField') THEN 'NpyArray' ELSE e->>'Contract' END) ORDER BY (e->'Ref'->>'artifact_id')::uuid,e->'Ref'->>'sha256',e->>'Contract') FROM jsonb_array_elements(r.config_snapshot->'v27_scene_prepare'->'input_objects') e)
 THEN RETURN 'conflict';END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.ordinal),'[]') INTO actual
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal<1000000;
 IF NOT (t.execution_profile ? 'v2_input_manifest') THEN
  IF r.status='draft' AND NOT r.graph_sealed AND t.state='blocked' AND t.attempt=0 AND t.canonical_result_id IS NULL AND actual='[]' AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task)
   AND t.execution_profile=p_profile AND t.inputs='[]'::jsonb THEN RETURN 'missing';END IF;
  RETURN 'conflict';
 END IF;
 IF actual IS DISTINCT FROM p_rows OR t.inputs IS DISTINCT FROM jsonb_build_array(p_manifest)||p_direct
 OR t.execution_profile->'v2_input_manifest' IS DISTINCT FROM p_manifest
 OR t.execution_profile->>'v2_entry_count' IS DISTINCT FROM p_count::text
 OR t.execution_profile->>'v2_entries_sha' IS DISTINCT FROM p_entries_sha
 OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM p_profile
 THEN RETURN 'conflict';END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256) ORDER BY i.ordinal),'[]') INTO transport
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal>=1000000;
 expected_transport:=p_chunks||jsonb_build_array(p_manifest);
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal>=1000000
 AND (i.ordinal NOT BETWEEN 1000000 AND 1000000+jsonb_array_length(p_chunks)
 OR i.contract IS DISTINCT FROM CASE WHEN i.ordinal=1000000+jsonb_array_length(p_chunks) THEN 'InputClosureManifest' ELSE 'InputClosureChunk' END)) THEN RETURN 'conflict';END IF;
 IF transport IS DISTINCT FROM expected_transport OR NOT photo.v2_input_closure_ready(p_task) THEN RETURN 'conflict';END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(expected_transport) WITH ORDINALITY ref(value,n)
 WHERE NOT EXISTS(SELECT 1 FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_task
 AND b.state='ready' AND b.artifact_id=(ref.value->>'artifact_id')::uuid AND b.sha256=ref.value->>'sha256'
 AND b.purpose=CASE WHEN ref.n=jsonb_array_length(expected_transport) THEN 'v2_input_manifest' ELSE 'v2_input_chunk_'||(ref.n-1)::text END)) THEN RETURN 'conflict';END IF;
 RETURN 'replayed';
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN 'conflict';
END $$;
CREATE FUNCTION photo.semantic_scene_registration_binding_state(p_tenant uuid,p_run uuid,p_binding jsonb)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE transport jsonb;params jsonb;count_entries integer;ref jsonb;row_entry jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_binding) IS DISTINCT FROM 'object' OR octet_length(p_binding::text)>4194304
 OR p_binding->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_binding-'schema_version'-'task_id'-'params'-'input_digest'-'profile'-'transport')<>'{}'
 OR jsonb_typeof(p_binding->'task_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_binding->'input_digest') IS DISTINCT FROM 'string'
 OR coalesce(p_binding->>'input_digest','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_binding->'profile') IS DISTINCT FROM 'object' THEN RETURN 'conflict';END IF;
 params:=p_binding->'params';transport:=p_binding->'transport';
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR (params-'artifact_id'-'sha256')<>'{}'
 OR jsonb_typeof(params->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'sha256') IS DISTINCT FROM 'string'
 OR coalesce(params->>'sha256','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(transport) IS DISTINCT FROM 'object'
 OR transport->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (transport-'schema_version'-'rows'-'manifest'-'chunks'-'direct'-'count'-'entries_sha')<>'{}'
 OR jsonb_typeof(transport->'count') IS DISTINCT FROM 'number'
 OR coalesce(transport->>'count','') !~ '^[0-9]+$'
 OR jsonb_typeof(transport->'entries_sha') IS DISTINCT FROM 'string'
 OR coalesce(transport->>'entries_sha','') !~ '^[0-9a-f]{64}$'
 OR (p_binding->>'task_id')::uuid='00000000-0000-0000-0000-000000000000'
 OR (params->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000' THEN RETURN 'conflict';END IF;
 count_entries:=(transport->>'count')::integer;
 IF count_entries NOT BETWEEN 3 AND 4097 THEN RETURN 'conflict';END IF;
 IF jsonb_typeof(transport->'rows') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'rows')<>count_entries
 OR jsonb_typeof(transport->'chunks') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'chunks') NOT BETWEEN 1 AND 4097
 OR jsonb_typeof(transport->'direct') IS DISTINCT FROM 'array' OR jsonb_array_length(transport->'direct')<>3
 OR jsonb_typeof(transport->'manifest') IS DISTINCT FROM 'object' THEN RETURN 'conflict';END IF;
 FOR row_entry IN SELECT value FROM jsonb_array_elements(transport->'rows') LOOP
  IF jsonb_typeof(row_entry) IS DISTINCT FROM 'object' OR (row_entry-ARRAY['ref','contract'])<>'{}'
  OR row_entry->>'contract' IS NULL OR row_entry->>'contract' NOT IN ('Params','WorkingImage','NpyArray') THEN RETURN 'conflict';END IF;
 END LOOP;
 FOR ref IN SELECT row_entry.value->'ref' FROM jsonb_array_elements(transport->'rows') row_entry
 UNION ALL SELECT value FROM jsonb_array_elements(transport->'chunks')
 UNION ALL SELECT value FROM jsonb_array_elements(transport->'direct')
 UNION ALL SELECT transport->'manifest' LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-ARRAY['artifact_id','sha256'])<>'{}'
  OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR (ref->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000' THEN RETURN 'conflict';END IF;
 END LOOP;
 IF (SELECT count(DISTINCT value->'ref'->>'artifact_id') FROM jsonb_array_elements(transport->'rows'))<>count_entries
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(transport->'direct'))<>3 THEN RETURN 'conflict';END IF;
 RETURN photo.semantic_scene_preparation_registration_state(p_tenant,p_run,(p_binding->>'task_id')::uuid,
  (params->>'artifact_id')::uuid,params->>'sha256',p_binding->>'input_digest',p_binding->'profile',
  transport->'rows',transport->'manifest',transport->'chunks',transport->'direct',count_entries,transport->>'entries_sha');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN 'conflict';
END $$;
ALTER FUNCTION photo.semantic_scene_preparation_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.semantic_scene_registration_binding_state(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_scene_preparation_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION photo.semantic_scene_registration_binding_state(uuid,uuid,jsonb) FROM PUBLIC;
DO $acl$ DECLARE f record; entry record; BEGIN
 FOR f IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname IN('semantic_scene_preparation_registration_state','semantic_scene_registration_binding_state') LOOP
  FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x WHERE p.oid=f.oid AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',f.oid::regprocedure,(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
  END LOOP;
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.semantic_scene_registration_binding_state(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- Atomic scene admission: no task is activated until all current proofs,
-- exact registration, original receipt and live reservation agree.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_semantic_scene_preparation(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;i photo.idempotency_records%ROWTYPE;t photo.tasks%ROWTYPE;
 q photo.quota_reservations%ROWTYPE;c jsonb;actor uuid;session_id uuid;project uuid;ticket jsonb;admission_key text;
 next_cursor bigint;event_id uuid;changed integer;reservation_count integer:=0;output_bytes numeric;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_packet) IS DISTINCT FROM 'object' OR octet_length(p_packet::text)>33554432
 OR p_packet->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_packet-ARRAY['schema_version','tenant_id','project_id','actor_id','session_id','run_id','request_sha','config_text','policy_text','registration'])<>'{}'::jsonb
 OR p_packet->>'tenant_id' IS DISTINCT FROM p_tenant::text OR p_packet->>'run_id' IS DISTINCT FROM p_run::text
 OR jsonb_typeof(p_packet->'config_text') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_packet->'policy_text') IS DISTINCT FROM 'string' THEN RETURN NULL;END IF;
 IF photo.lock_semantic_scene_scope(p_tenant,p_run,(p_packet->'registration'->>'task_id')::uuid) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 c:=r.config_snapshot->'v27_scene_prepare';actor:=r.created_by;session_id:=(c->>'auth_session_id')::uuid;project:=(r.config_snapshot->>'project_id')::uuid;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(c->>'task_id')::uuid FOR UPDATE;
 IF w.id IS NULL OR t.id IS NULL OR r.failure_policy<>'fail_fast'
 OR p_packet->>'project_id' IS DISTINCT FROM project::text OR p_packet->>'actor_id' IS DISTINCT FROM actor::text
 OR p_packet->>'session_id' IS DISTINCT FROM session_id::text OR p_packet->'request_sha' IS DISTINCT FROM c->'request_sha'
 OR (p_packet->>'config_text')::jsonb IS DISTINCT FROM r.config_snapshot
 OR encode(public.digest(convert_to(p_packet->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR (p_packet->>'policy_text')::jsonb IS DISTINCT FROM c->'qualification_expected'
 OR encode(public.digest(convert_to(p_packet->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha
 OR photo.semantic_scene_registration_binding_state(p_tenant,p_run,p_packet->'registration') IS DISTINCT FROM 'replayed'
 OR EXISTS(SELECT 1 FROM photo.workflow_consent_links WHERE tenant_id=p_tenant AND workflow_id=w.id)
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run) THEN RETURN NULL;END IF;
 admission_key:='scene-prep:'||p_run::text;
 SELECT * INTO i FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=actor
 AND route='V2.Admit' AND idempotency_records.key=admission_key FOR UPDATE;
 IF NOT FOUND OR i.resource_id IS DISTINCT FROM p_run OR i.request_hash IS DISTINCT FROM c->>'request_sha' THEN RETURN NULL;END IF;
 ticket:=jsonb_build_object('run_id',p_run,'workflow_id',w.id,'state','queued');
 IF r.graph_sealed THEN
  IF i.response_status=202 AND i.response_body=ticket
  AND photo.lock_semantic_scene_scope(p_tenant,p_run,t.id) IS TRUE THEN RETURN ticket;END IF;
  RETURN NULL;
 END IF;
 IF r.status<>'draft' OR t.state<>'blocked' OR t.attempt<>0 OR t.canonical_result_id IS NOT NULL
 OR i.response_status IS NOT NULL OR i.expires_at<=clock_timestamp()
 OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation AND op.version=t.operation_version
  AND op.v2_enabled AND op.resource_class='gpu' AND op.backends ? 'cuda')
 OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN NULL;END IF;
 output_bytes:=(t.execution_profile->'resource_estimate'->>'output_bytes')::numeric;
 IF (output_bytes BETWEEN 1 AND 9007199254740991) IS NOT TRUE
 OR ((t.execution_profile->'resource_estimate'->>'vram_bytes')::numeric BETWEEN 1 AND 10000000000) IS NOT TRUE THEN RETURN NULL;END IF;
 FOR q IN SELECT * FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run ORDER BY id FOR UPDATE LOOP
  reservation_count:=reservation_count+1;
  IF q.state<>'reserved' OR q.expires_at<=clock_timestamp() OR q.principal_type<>'user' OR q.principal_id IS DISTINCT FROM actor OR q.share_id IS NOT NULL OR q.files<>0
  OR q.bytes::numeric<output_bytes+8388608 OR q.bytes IS DISTINCT FROM (c->>'reserved_bytes')::bigint
  OR NOT EXISTS(SELECT 1 FROM photo.tenant_usage WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes) THEN RETURN NULL;END IF;
 END LOOP;
 -- Acquire the event cursor before the final clock fence. Waiting for an
 -- unrelated event writer must not outlive source/report/session/quota authority.
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 PERFORM tenant_id FROM photo.tenant_event_cursors WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF reservation_count<>1 OR photo.lock_semantic_scene_scope(p_tenant,p_run,t.id) IS NOT TRUE
 OR i.expires_at<=clock_timestamp()
 OR EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run AND expires_at<=clock_timestamp()) THEN RETURN NULL;END IF;
 UPDATE photo.runs SET graph_sealed=true,status='queued',revision=revision+1
 WHERE tenant_id=p_tenant AND id=p_run AND status='draft' AND NOT graph_sealed AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SCENE_SEAL_RACE' USING ERRCODE='40001';END IF;
 IF photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) IS NOT TRUE THEN
  RAISE EXCEPTION 'SEMANTIC_SCENE_ACTIVATION_FAILED' USING ERRCODE='23514';END IF;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(p_tenant,next_cursor,p_run,actor,'principal','v2.run.queued',jsonb_build_object('run_id',p_run,'status','queued','revision',r.revision+1));
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(p_tenant,event_id,'photo.events.v1','v2.run.queued',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.queued','tenant_id',p_tenant,'run_id',p_run,'status','queued'));
 UPDATE photo.idempotency_records SET response_status=202,response_body=ticket WHERE tenant_id=p_tenant AND id=i.id AND response_status IS NULL;
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>1 THEN RAISE EXCEPTION 'V27_NATIVE_TICKET_RACE' USING ERRCODE='40001';END IF;
 RETURN ticket;
END $$;
ALTER FUNCTION photo.seal_semantic_scene_preparation(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_scene_preparation(uuid,uuid,jsonb) FROM PUBLIC;
DO $acl$ DECLARE entry record; BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.seal_semantic_scene_preparation(uuid,uuid,jsonb)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.seal_semantic_scene_preparation(uuid,uuid,jsonb) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.seal_semantic_scene_preparation(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- Authorized immutable recovery projections. No latest-analysis discovery.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_scene_saved_config(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_run uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT config_snapshot->'v27_scene_prepare' INTO c FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF c IS NULL OR c->>'parent_run_id' IS DISTINCT FROM p_parent::text OR c->>'request_sha' IS DISTINCT FROM p_request_sha
 OR c->>'auth_session_id' IS DISTINCT FROM p_session::text THEN RETURN NULL;END IF;
 IF photo.lock_semantic_scene_scope(p_tenant,p_run,(c->>'task_id')::uuid) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(c->>'task_id')::uuid;
 IF r.created_by IS DISTINCT FROM p_actor OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('config_snapshot',r.config_snapshot,'config_hash',r.config_hash,'run_state',r.status,'task_state',t.state,'sealed',r.graph_sealed);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
CREATE FUNCTION photo.semantic_scene_completed_result(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;c jsonb;
 a photo.artifacts%ROWTYPE;root jsonb;pixels jsonb;invalid jsonb:='null'::jsonb;ref jsonb;objects jsonb;
 root_role text;pixel_role text;seen integer:=0;expected integer;params_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.lock_semantic_scene_scope(p_tenant,p_run,p_task) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task;
 c:=r.config_snapshot->'v27_scene_prepare';params_id:=(c->'params'->>'artifact_id')::uuid;
 IF NOT r.graph_sealed OR r.status NOT IN('queued','running','waiting_review','completed')
 OR r.created_by IS DISTINCT FROM p_actor OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR c->>'auth_session_id' IS DISTINCT FROM p_session::text OR t.state<>'succeeded' OR t.attempt<1 OR t.fencing_token<1
 OR t.canonical_result_id IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=p_task AND id=t.canonical_result_id
 AND attempt=t.attempt AND fencing_token=t.fencing_token FOR SHARE;
 IF NOT FOUND OR jsonb_typeof(result.outputs) IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 expected:=jsonb_array_length(result.outputs);
 IF expected NOT BETWEEN 2 AND 3 OR (t.operation='scene.parse' AND expected<>2)
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(result.outputs))<>expected THEN RETURN NULL;END IF;
 root_role:=CASE WHEN t.operation='scene.parse' THEN 'scene_labels_ref' ELSE 'relative_depth_ref' END;
 pixel_role:=CASE WHEN t.operation='scene.parse' THEN 'scene_labels' ELSE 'relative_depth' END;
 -- Lock the whole canonical output set in stable order before validating it.
 PERFORM x.id FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id IN(
  SELECT (value->>'artifact_id')::uuid FROM jsonb_array_elements(result.outputs)) ORDER BY x.id FOR SHARE OF x;
 FOR ref IN SELECT value FROM jsonb_array_elements(result.outputs) LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-ARRAY['artifact_id','sha256'])<>'{}'::jsonb THEN RETURN NULL;END IF;
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256';
  IF NOT FOUND OR a.id=params_id OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes NOT BETWEEN 1 AND 9007199254740991
  OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())
  OR a.created_by_task_id IS DISTINCT FROM t.id OR a.produced_attempt IS DISTINCT FROM t.attempt OR a.produced_fence IS DISTINCT FROM t.fencing_token
  OR a.bucket IS DISTINCT FROM c->'qualification_report'->>'bucket' THEN RETURN NULL;END IF;
  IF a.output_role=root_role AND a.media_type='application/json' AND a.bytes<=8388608 AND root IS NULL THEN root:=ref;
  ELSIF a.output_role=pixel_role AND a.media_type IN('application/octet-stream','application/x-npy') AND pixels IS NULL THEN pixels:=ref;
  ELSIF t.operation='depth.predict' AND a.output_role='depth_invalid' AND a.media_type IN('application/octet-stream','application/x-npy') AND invalid='null'::jsonb THEN invalid:=ref;
  ELSE RETURN NULL;END IF;
  seen:=seen+1;
 END LOOP;
 IF root IS NULL OR pixels IS NULL OR seen<>expected OR seen<>(2+CASE WHEN invalid='null'::jsonb THEN 0 ELSE 1 END) THEN RETURN NULL;END IF;
 -- All input/output/qualification locks have now been acquired. Repeat the
 -- clock-sensitive scope and object predicates before issuing physical locators.
 IF photo.lock_semantic_scene_scope(p_tenant,p_run,p_task) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',x.id,'sha256',x.sha256),'key',x.object_key,
  'version',x.object_version,'bytes',x.bytes,'media_type',x.media_type) ORDER BY x.id) INTO objects
 FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id IN(params_id,(root->>'artifact_id')::uuid,(pixels->>'artifact_id')::uuid,(invalid->>'artifact_id')::uuid)
 AND x.state='ready' AND x.verified_at IS NOT NULL AND x.bytes>0 AND x.object_version IS NOT NULL AND x.object_version NOT IN('','null')
 AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND x.bucket=c->'qualification_report'->>'bucket';
 IF jsonb_array_length(objects) IS DISTINCT FROM expected+1 THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run_id',p_run,'task_id',p_task,'result_id',result.id,'attempt',t.attempt,'fence',t.fencing_token,
  'config_sha',r.config_hash,'params',c->'params','root',root,'pixels',pixels,'invalid',invalid,'objects',objects);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.semantic_scene_saved_config(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.semantic_scene_completed_result(uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_scene_saved_config(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION photo.semantic_scene_completed_result(uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
DO $acl$ DECLARE f record; entry record; BEGIN
 FOR f IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname IN('semantic_scene_saved_config','semantic_scene_completed_result') LOOP
  FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x WHERE p.oid=f.oid AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',f.oid::regprocedure,(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
  END LOOP;
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.semantic_scene_saved_config(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api,photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.semantic_scene_completed_result(uuid,uuid,uuid,uuid,uuid,uuid) TO photo_api,photo_scheduler;

-- Scene preparation settles its own reservation; canonical task adoption alone
-- must not leave a completed producer holding capacity indefinitely.
CREATE FUNCTION photo.finish_semantic_scene_preparation(p_tenant uuid,p_run uuid,p_task uuid,p_result uuid,p_attempt integer,p_fence bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 c jsonb;proof jsonb;next_cursor bigint;event_id uuid;
BEGIN
 IF photo.lock_semantic_scene_scope(p_tenant,p_run,p_task) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task;
 c:=r.config_snapshot->'v27_scene_prepare';
 IF t.canonical_result_id IS DISTINCT FROM p_result OR t.attempt IS DISTINCT FROM p_attempt
 OR t.fencing_token IS DISTINCT FROM p_fence THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run)<>1 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF q.principal_type<>'user' OR q.principal_id IS DISTINCT FROM r.created_by OR q.share_id IS NOT NULL OR q.files<>0 THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks x ON x.tenant_id=u.tenant_id AND x.id=u.task_id
 WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND u.state NOT IN('ready','cleaned'))
 OR EXISTS(SELECT 1 FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND state NOT IN('ready','cleaned')) THEN RETURN false;END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 PERFORM tenant_id FROM photo.tenant_event_cursors WHERE tenant_id=p_tenant FOR UPDATE;
 -- This proof follows every potentially blocking lock, including the cursor.
 proof:=photo.semantic_scene_completed_result(p_tenant,(r.config_snapshot->>'project_id')::uuid,r.created_by,(c->>'auth_session_id')::uuid,p_run,p_task);
 IF proof IS NULL OR proof->>'result_id' IS DISTINCT FROM p_result::text
 OR (proof->>'attempt')::integer IS DISTINCT FROM p_attempt OR (proof->>'fence')::bigint IS DISTINCT FROM p_fence THEN RETURN false;END IF;
 IF r.status='completed' THEN RETURN q.state='released' AND q.bytes=0;END IF;
 IF r.status NOT IN('queued','running','waiting_review') OR r.revision>=9007199254740991
 OR q.state<>'reserved' OR q.bytes<0 OR q.expires_at<=clock_timestamp()
 -- begin_output moves bytes out of the remaining run reservation into upload
 -- allocations. Cleanup can return an allocation once; never count it twice.
 OR q.bytes::numeric+coalesce((SELECT sum(u.bytes)::numeric FROM photo.output_uploads u
 JOIN photo.tasks allocated ON allocated.tenant_id=u.tenant_id AND allocated.id=u.task_id
 WHERE u.tenant_id=q.tenant_id AND u.run_reservation_id=q.id AND NOT u.returned_to_run_reservation
 AND allocated.run_id=r.id),0) IS DISTINCT FROM (c->>'reserved_bytes')::numeric THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'SCENE_PREPARATION_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
 UPDATE photo.quota_reservations SET state='released',bytes=0,expires_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=q.id AND state='reserved';
 IF NOT FOUND THEN RAISE EXCEPTION 'SCENE_PREPARATION_RESERVATION_RACE' USING ERRCODE='40001';END IF;
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run
 AND status=r.status AND revision=r.revision AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'SCENE_PREPARATION_FINISH_RACE' USING ERRCODE='40001';END IF;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,p_run,r.created_by,'principal','v2.run.completed',jsonb_build_object('run_id',p_run,'status','completed','revision',r.revision+1));
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v2.run.completed',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.completed','tenant_id',p_tenant,'run_id',p_run,'status','completed'));
 RETURN true;
END $$;
ALTER FUNCTION photo.finish_semantic_scene_preparation(uuid,uuid,uuid,uuid,integer,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_semantic_scene_preparation(uuid,uuid,uuid,uuid,integer,bigint) FROM PUBLIC;
DO $acl$ DECLARE entry record;BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.finish_semantic_scene_preparation(uuid,uuid,uuid,uuid,integer,bigint)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION photo.finish_semantic_scene_preparation(uuid,uuid,uuid,uuid,integer,bigint) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.finish_semantic_scene_preparation(uuid,uuid,uuid,uuid,integer,bigint) TO photo_scheduler;

ALTER FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) RENAME TO v2_pending_shell_matches_pre_v27_scene;
CREATE FUNCTION photo.v2_pending_shell_matches(p_tenant uuid,p_run uuid,p_workflow uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_type text,p_policy_sha text,p_config_sha text,p_config jsonb,p_tasks jsonb,p_edges jsonb,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c jsonb;item jsonb;profile jsonb;task uuid;
BEGIN
 IF NOT (p_config ? 'v27_scene_prepare') THEN
 RETURN photo.v2_pending_shell_matches_pre_v27_scene(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,p_tasks,p_edges,p_bytes);
 END IF;
 c:=p_config->'v27_scene_prepare';
 IF p_type IS DISTINCT FROM 'semantic' OR photo.semantic_scene_marker_identity(p_run,c) IS NOT TRUE
 OR jsonb_typeof(p_tasks) IS DISTINCT FROM 'array' OR jsonb_array_length(p_tasks)<>1
 OR jsonb_typeof(p_edges) IS DISTINCT FROM 'array' OR jsonb_array_length(p_edges)<>0
 OR p_config ?| ARRAY['v27_face_prepare','v27_person_prepare','v27_defect_prepare','v27_detector_prepare','v27_skin_prepare','v27_defect_native_prepare','v27_target_person_prepare','v27_target_detector_prepare','v27_target_face_prepare','v27_target_skin_prepare','v27_target_defect_prepare','v27_native_targets_transfer']
 OR p_bytes IS DISTINCT FROM (c->>'reserved_bytes')::bigint THEN RETURN false;END IF;
 item:=p_tasks->0;
 IF item IS DISTINCT FROM c->'task' THEN RETURN false;END IF;
 task:=(c->>'task_id')::uuid;
 SELECT execution_profile INTO profile FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=task;
 IF NOT FOUND THEN RETURN false;END IF;
 IF profile ? 'v2_input_manifest' THEN
 IF item->'execution_profile' IS DISTINCT FROM (profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
 OR NOT photo.v2_input_closure_ready(task) THEN RETURN false;END IF;
 item:=jsonb_set(item,'{execution_profile}',profile);
 END IF;
 RETURN photo.v2_pending_shell_matches_pre_v27_registered(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,jsonb_build_array(item),p_edges,p_bytes);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) FROM PUBLIC;
DO $acl$ DECLARE f record;entry record;BEGIN
 FOR f IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname IN('v2_pending_shell_matches','v2_pending_shell_matches_pre_v27_scene') LOOP
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x WHERE p.oid=f.oid AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',f.oid::regprocedure,(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
 END LOOP;
END $acl$;

-- A scene producer has a typed root plus binary pixels, unlike a renderer's
-- single Document root. Validate the actual uploaded set before result insert.
CREATE FUNCTION photo.semantic_scene_result_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_outputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;c jsonb;ref jsonb;a photo.artifacts%ROWTYPE;
 seen uuid[]:='{}';roles text[]:='{}';aid uuid;root_role text;pixel_role text;expected integer;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.operation NOT IN('scene.parse','depth.predict') OR t.operation_version<>'1'
 OR t.resource_class<>'gpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cuda'
 OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR p_attempt<1 OR p_fence<1 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 c:=r.config_snapshot->'v27_scene_prepare';
 IF r.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.status<>'running'
 OR photo.semantic_scene_marker_identity(r.id,c) IS NOT TRUE OR c->>'task_id' IS DISTINCT FROM t.id::text
 OR c->>'operation' IS DISTINCT FROM t.operation OR c->>'input_digest' IS DISTINCT FROM t.input_digest
 OR c->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',c->'task'->>'params_sha')
 OR c->'task'->'execution_profile' IS DISTINCT FROM (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
 OR NOT photo.v2_input_closure_ready(t.id) OR jsonb_typeof(p_outputs) IS DISTINCT FROM 'array'
 OR octet_length(p_outputs::text)>8192 THEN RETURN false;END IF;
 expected:=jsonb_array_length(p_outputs);
 IF expected NOT BETWEEN 2 AND 3 OR (t.operation='scene.parse' AND expected<>2) THEN RETURN false;END IF;
 root_role:=CASE WHEN t.operation='scene.parse' THEN 'scene_labels_ref' ELSE 'relative_depth_ref' END;
 pixel_role:=CASE WHEN t.operation='scene.parse' THEN 'scene_labels' ELSE 'relative_depth' END;
 FOR ref IN SELECT value FROM jsonb_array_elements(p_outputs) LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-ARRAY['artifact_id','sha256'])<>'{}'::jsonb
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid IS NULL OR aid='00000000-0000-0000-0000-000000000000'::uuid OR aid=ANY(seen) OR aid=t.params_artifact_id THEN RETURN false;END IF;
  seen:=array_append(seen,aid);
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=aid AND sha256=ref->>'sha256';
  IF NOT FOUND OR a.output_role=ANY(roles) OR a.state<>'ready' OR a.verified_at IS NULL
  OR a.bytes NOT BETWEEN 1 AND 9007199254740991 OR a.object_version IS NULL OR a.object_version IN('','null')
  OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())
  OR a.created_by_task_id IS DISTINCT FROM t.id OR a.produced_attempt IS DISTINCT FROM p_attempt OR a.produced_fence IS DISTINCT FROM p_fence
  OR a.bucket IS DISTINCT FROM c->'qualification_report'->>'bucket' THEN RETURN false;END IF;
  IF a.output_role=root_role THEN
   IF a.media_type<>'application/json' OR a.bytes>8388608 THEN RETURN false;END IF;
  ELSIF a.output_role=pixel_role OR (t.operation='depth.predict' AND a.output_role='depth_invalid') THEN
   IF a.media_type NOT IN('application/octet-stream','application/x-npy') THEN RETURN false;END IF;
  ELSE RETURN false;END IF;
  roles:=array_append(roles,a.output_role);
  IF NOT EXISTS(SELECT 1 FROM photo.output_uploads u WHERE u.tenant_id=p_tenant AND u.artifact_id=a.id
   AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready' AND u.completed_at IS NOT NULL
   AND u.role=a.output_role AND u.bytes=a.bytes AND u.sha256=a.sha256 AND u.bucket=a.bucket
   AND u.object_key=a.object_key AND u.object_version=a.object_version AND u.media_type=a.media_type) THEN RETURN false;END IF;
 END LOOP;
 RETURN root_role=ANY(roles) AND pixel_role=ANY(roles)
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id=ANY(seen)
 AND (x.state<>'ready' OR (x.expires_at IS NOT NULL AND x.expires_at<=clock_timestamp())));
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_scene_result_ready(uuid,uuid,integer,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_scene_result_ready(uuid,uuid,integer,bigint,jsonb) FROM PUBLIC;
DO $acl$ DECLARE entry record;BEGIN
 FOR entry IN SELECT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) x
 WHERE p.oid='photo.semantic_scene_result_ready(uuid,uuid,integer,bigint,jsonb)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION photo.semantic_scene_result_ready(uuid,uuid,integer,bigint,jsonb) FROM %I',(SELECT rolname FROM pg_roles WHERE oid=entry.grantee));
 END LOOP;
END $acl$;
DO $install$
DECLARE source text;definition text;
 needle text:=' IF task_row.operation=''defects.propose'' AND run_row.config_snapshot ? ''v27_target_defect_prepare'' THEN';
 branch text:=$branch$ IF task_row.operation IN('scene.parse','depth.predict') AND run_row.config_snapshot ? 'v27_scene_prepare' THEN
  IF photo.semantic_scene_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_SCENE_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';END IF;
  RETURN NEW;
 END IF;
$branch$;
BEGIN
 SELECT p.prosrc,pg_get_functiondef(p.oid) INTO source,definition FROM pg_proc p
 WHERE p.oid='photo.guard_v2_result_root()'::regprocedure AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner';
 IF source IS NULL OR (length(source)-length(replace(source,needle,'')))/length(needle)<>1
 OR strpos(source,'semantic_scene_result_ready')>0 THEN RAISE EXCEPTION 'SCENE_RESULT_TRIGGER_SOURCE_DRIFT';END IF;
 EXECUTE replace(definition,source,replace(source,needle,branch||needle));
END $install$;

-- Extend the actual native start wrapper inside the installed claim chain.
-- Its existing transaction promotes queued children only after a valid claim.
DO $scene_start$
DECLARE target oid;matches integer;source text;definition text;
 stage text:='   WHEN ''face.detect'' THEN ''v27_detector_prepare''';
 model text:=' IF task_row.operation IN(''mask.person'',''face.detect'',''face.landmarks'',''face.parse'')';
BEGIN
 SELECT count(*),min(p.oid::bigint)::oid INTO matches,target FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND strpos(p.prosrc,'stage_name:=CASE task_row.operation')>0;
 IF matches<>1 THEN RAISE EXCEPTION 'SCENE_NATIVE_START_SOURCE_AMBIGUOUS';END IF;
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc WHERE oid=target
 AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
 IF source IS NULL OR (length(source)-length(replace(source,stage,'')))/length(stage)<>1
 OR (length(source)-length(replace(source,model,'')))/length(model)<>1
 OR strpos(source,'v27_scene_prepare')>0 THEN RAISE EXCEPTION 'SCENE_NATIVE_START_SOURCE_DRIFT';END IF;
 source:=replace(source,stage,'   WHEN ''scene.parse'' THEN ''v27_scene_prepare'''||chr(10)||'   WHEN ''depth.predict'' THEN ''v27_scene_prepare'''||chr(10)||stage);
 source:=replace(source,model,' IF task_row.operation IN(''mask.person'',''face.detect'',''face.landmarks'',''face.parse'',''scene.parse'',''depth.predict'')');
 -- pg_get_functiondef preserves the original signature, owner and wrapper calls.
 EXECUTE replace(definition,(SELECT prosrc FROM pg_proc WHERE oid=target),source);
END $scene_start$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
