-- +goose Up
-- Exact lease-scoped metadata ports only. No table grants or old function edits.
-- +goose StatementBegin
DO $guard$
BEGIN
 IF (SELECT max(version_id)FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 2653 THEN RAISE EXCEPTION 'APPLY_LOCATOR_HEAD_REQUIRED' USING ERRCODE='55000';END IF;
 IF EXISTS(SELECT FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname IN('apply_input_style_locator','apply_artifact_locator','apply_conservative_report_locator')) THEN RAISE EXCEPTION 'APPLY_LOCATOR_NAME_EXISTS' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_inputs(uuid,uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'b7e49c267c73eeb6efa19f83ba2fac50712ffbf9a558a36e06cba3caa8e4ec37' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_stage_outputs(uuid,uuid,uuid,text,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'e73d673abd2d22d742a6808f983698cdfd608192bfa2b690f9a962f0ef0ecb33' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_v4_inputs(uuid,uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '4fdbdf0eac9fe83a0f42c0d71174453700bcee3055e0a39bcf5507fdf5e92461' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_v4_inputs_with_metadata(uuid,uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'e19e3b5ef853cd25d5f74a508d2843e0efaf7aa85cb5808cc3cdaec10ba0b797' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_v4_stage_outputs(uuid,uuid,uuid,text,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '05d9c3fb4cf4f4130668638caaf24a4719cc8d27a5a8f37dee2ac5e6c797b0f0' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.authored_render_base_adoption_context(uuid,uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '4444e5e5d45274f0714aa2e910a00168f8bb85d746576fd43b7f76d933cc990a' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.claim_apply_build_versioned()'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '9e1bb31419af1892f7d4ca54150199f5e6b23c263ee38332d903b3b025654248' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.renew_apply_build(uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'c21c7f7a203e1bf0cbf6df4adafe466a50cfdf59e85e419462284152ab454b8c' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
END $guard$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.apply_input_style_locator(p_tenant uuid,p_run uuid,p_token uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $locator$
DECLARE b photo.apply_builds%ROWTYPE; admitted boolean;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_token IS NULL OR p_artifact IS NULL OR p_sha IS NULL OR p_sha!~'^[a-f0-9]{64}$' THEN RETURN;END IF;
 SELECT x.* INTO b FROM photo.apply_builds x JOIN photo.runs r ON r.tenant_id=x.tenant_id AND r.id=x.run_id
 JOIN photo.tenants t ON t.id=r.tenant_id AND t.status='active'
 WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.lease_token=p_token AND x.lease_until>clock_timestamp()
 AND x.graph_version IN(3,4) AND x.phase NOT IN('complete','failed') AND r.kind='apply'
 AND r.status IN('queued','running','waiting_review') AND NOT r.cancel_requested;
 IF NOT FOUND THEN RETURN;END IF;
 IF b.graph_version=4 THEN
  SELECT EXISTS(SELECT FROM photo.apply_v4_inputs_with_metadata(p_tenant,p_run,p_token) i WHERE i.style_artifact_id=p_artifact AND i.style_sha256=p_sha) INTO admitted;
 ELSE
  SELECT EXISTS(SELECT FROM photo.apply_inputs(p_tenant,p_run,p_token) i WHERE i.style_artifact_id=p_artifact AND i.style_sha256=p_sha) INTO admitted;
 END IF;
 IF admitted IS NOT TRUE THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.style_versions sv JOIN photo.artifacts a ON a.tenant_id=sv.tenant_id AND a.id=sv.export_artifact_id AND a.sha256=sv.config_hash
 WHERE sv.tenant_id=p_tenant AND sv.id=b.style_version_id AND sv.approval_status='accepted'
 AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
 AND a.bucket<>'' AND a.object_key<>'' AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.media_type<>'' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
END $locator$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.apply_artifact_locator(p_tenant uuid,p_run uuid,p_token uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $locator$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;admitted boolean:=false;origin_valid boolean:=false;context_row record;authored_original jsonb;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_token IS NULL OR p_artifact IS NULL OR p_sha IS NULL OR p_sha!~'^[a-f0-9]{64}$' THEN RETURN;END IF;
 SELECT x.* INTO b FROM photo.apply_builds x JOIN photo.runs y ON y.tenant_id=x.tenant_id AND y.id=x.run_id
 JOIN photo.tenants t ON t.id=y.tenant_id AND t.status='active'
 WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.lease_token=p_token AND x.lease_until>clock_timestamp()
 AND x.graph_version IN(3,4) AND x.phase NOT IN('complete','failed') AND y.kind='apply'
 AND y.status IN('queued','running','waiting_review') AND NOT y.cancel_requested;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 -- Only the existing authored adoption constructor authorizes its stored
 -- cross-run original source. Never admit arbitrary parent outputs or JSON refs.
 IF b.phase='adopt' AND (r.config_snapshot?'authored_render_origin' OR EXISTS(SELECT FROM photo.authored_render_base_children c WHERE c.tenant_id=p_tenant AND c.child_run_id=p_run)) THEN
  SELECT * INTO STRICT context_row FROM photo.authored_render_base_adoption_context(p_tenant,p_run,p_token);
  IF context_row.authored IS DISTINCT FROM true THEN RETURN;END IF;
  origin_valid:=true;authored_original:=context_row.body#>'{original_source,source}';
  admitted:=authored_original=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha);
 ELSE
  IF b.graph_version=4 THEN
   SELECT EXISTS(SELECT FROM photo.apply_v4_inputs_with_metadata(p_tenant,p_run,p_token)) INTO origin_valid;
   SELECT EXISTS(SELECT FROM photo.apply_v4_inputs_with_metadata(p_tenant,p_run,p_token) i WHERE i.artifact_id=p_artifact AND i.sha256=p_sha) INTO admitted;
  ELSE
   SELECT EXISTS(SELECT FROM photo.apply_inputs(p_tenant,p_run,p_token)) INTO origin_valid;
   SELECT EXISTS(SELECT FROM photo.apply_inputs(p_tenant,p_run,p_token) i WHERE i.artifact_id=p_artifact AND i.sha256=p_sha) INTO admitted;
  END IF;
 END IF;
 IF origin_valid IS NOT TRUE THEN RETURN;END IF;
 -- Same-run canonical outputs must be registered Apply nodes, same current
 -- attempt/fence, explicit result.outputs membership and an already usable stage.
 IF admitted IS NOT TRUE THEN
  SELECT EXISTS(SELECT FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id AND t.operation=n.operation
   JOIN photo.task_results q ON q.tenant_id=t.tenant_id AND q.task_id=t.id AND q.id=t.canonical_result_id AND q.attempt=t.attempt AND q.fencing_token=t.fencing_token
   JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=p_artifact AND a.sha256=p_sha AND a.created_by_task_id=t.id AND a.produced_attempt=q.attempt AND a.produced_fence=q.fencing_token
   WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND t.state='succeeded' AND NOT t.cancel_requested
   AND q.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
   AND (b.graph_version=3 OR EXISTS(SELECT FROM photo.apply_v4_stages v WHERE v.tenant_id=n.tenant_id AND v.run_id=n.run_id AND v.phase=n.phase AND v.operation=n.operation AND (b.phase='adopt' OR v.stage_index<b.stage_index)))) INTO admitted;
 END IF;
 IF admitted IS NOT TRUE THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
 AND a.bucket<>'' AND a.object_key<>'' AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.media_type<>'' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
END $locator$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.apply_conservative_report_locator(p_tenant uuid,p_run uuid,p_token uuid,p_task uuid,p_proposal uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $locator$
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_token IS NULL OR p_task IS NULL OR p_proposal IS NULL OR p_sha IS NULL OR p_sha!~'^[a-f0-9]{64}$' THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.apply_builds b JOIN photo.runs run ON run.tenant_id=b.tenant_id AND run.id=b.run_id AND run.kind='apply'
 JOIN photo.tenants tenant ON tenant.id=b.tenant_id AND tenant.status='active'
 JOIN photo.apply_nodes n ON n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.id=p_task AND n.operation='defects.propose'
 JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.run_id=n.run_id AND t.operation=n.operation
 JOIN photo.task_results q ON q.tenant_id=t.tenant_id AND q.task_id=t.id AND q.id=t.canonical_result_id AND q.attempt=t.attempt AND q.fencing_token=t.fencing_token
 JOIN photo.artifacts p ON p.tenant_id=t.tenant_id AND p.id=p_proposal AND p.sha256=p_sha AND p.created_by_task_id=t.id
  AND p.produced_attempt=q.attempt AND p.produced_fence=q.fencing_token AND p.output_role='defect_candidates'
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=q.attempt AND a.produced_fence=q.fencing_token AND a.output_role='defect_propose_report'
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>clock_timestamp()
 AND b.graph_version=4 AND b.phase NOT IN('complete','failed') AND run.status IN('queued','running','waiting_review') AND NOT run.cancel_requested
 AND t.state='succeeded' AND NOT t.cancel_requested
 AND EXISTS(SELECT FROM photo.apply_v4_inputs_with_metadata(p_tenant,p_run,p_token))
 AND EXISTS(SELECT FROM photo.apply_v4_stages v WHERE v.tenant_id=n.tenant_id AND v.run_id=n.run_id AND v.phase=n.phase AND v.operation=n.operation AND (b.phase='adopt' OR v.stage_index<b.stage_index))
 AND p.state='ready' AND p.verified_at IS NOT NULL AND p.object_version IS NOT NULL AND p.object_version NOT IN('','null') AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0 AND a.bucket<>'' AND a.object_key<>'' AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.media_type<>'' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND q.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p.id,'sha256',p.sha256),jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
END $locator$;
-- +goose StatementEnd

ALTER FUNCTION photo.apply_input_style_locator(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_input_style_locator(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
-- +goose StatementBegin
DO $acl$
DECLARE role_name text;
BEGIN
 FOR role_name IN SELECT DISTINCT pg_get_userbyid(x.grantee) FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner)))x WHERE p.oid='photo.apply_input_style_locator(uuid,uuid,uuid,uuid,text)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.apply_input_style_locator(uuid,uuid,uuid,uuid,text) FROM %I',role_name);
 END LOOP;
END $acl$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION photo.apply_input_style_locator(uuid,uuid,uuid,uuid,text) TO photo_scheduler;

ALTER FUNCTION photo.apply_artifact_locator(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_artifact_locator(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
-- +goose StatementBegin
DO $acl$
DECLARE role_name text;
BEGIN
 FOR role_name IN SELECT DISTINCT pg_get_userbyid(x.grantee) FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner)))x WHERE p.oid='photo.apply_artifact_locator(uuid,uuid,uuid,uuid,text)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.apply_artifact_locator(uuid,uuid,uuid,uuid,text) FROM %I',role_name);
 END LOOP;
END $acl$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION photo.apply_artifact_locator(uuid,uuid,uuid,uuid,text) TO photo_scheduler;

ALTER FUNCTION photo.apply_conservative_report_locator(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_conservative_report_locator(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
-- +goose StatementBegin
DO $acl$
DECLARE role_name text;
BEGIN
 FOR role_name IN SELECT DISTINCT pg_get_userbyid(x.grantee) FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner)))x WHERE p.oid='photo.apply_conservative_report_locator(uuid,uuid,uuid,uuid,uuid,text)'::regprocedure AND x.grantee<>0 AND x.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION photo.apply_conservative_report_locator(uuid,uuid,uuid,uuid,uuid,text) FROM %I',role_name);
 END LOOP;
END $acl$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION photo.apply_conservative_report_locator(uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose StatementBegin
DO $post$
DECLARE f oid;role_row record;
BEGIN
 FOREACH f IN ARRAY ARRAY['photo.apply_input_style_locator(uuid,uuid,uuid,uuid,text)'::regprocedure::oid,'photo.apply_artifact_locator(uuid,uuid,uuid,uuid,text)'::regprocedure::oid,'photo.apply_conservative_report_locator(uuid,uuid,uuid,uuid,uuid,text)'::regprocedure::oid] LOOP
  IF NOT EXISTS(SELECT FROM pg_proc p JOIN pg_roles o ON o.oid=p.proowner WHERE p.oid=f AND o.rolname='photo_fn_owner' AND NOT o.rolcanlogin AND p.prosecdef AND p.proconfig IS NOT DISTINCT FROM ARRAY['search_path=pg_catalog, photo']::text[]) THEN RAISE EXCEPTION 'APPLY_LOCATOR_DEFINER_METADATA' USING ERRCODE='55000';END IF;
  IF EXISTS(SELECT FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner)))x WHERE p.oid=f AND (x.privilege_type<>'EXECUTE' OR x.is_grantable OR x.grantee=0 OR x.grantee NOT IN(p.proowner,'photo_scheduler'::regrole))) THEN RAISE EXCEPTION 'APPLY_LOCATOR_EXECUTE_SCOPE' USING ERRCODE='55000';END IF;
  IF NOT has_function_privilege('photo_scheduler',f,'EXECUTE') THEN RAISE EXCEPTION 'APPLY_LOCATOR_SCHEDULER_REQUIRED' USING ERRCODE='55000';END IF;
  FOR role_row IN SELECT oid FROM pg_roles WHERE NOT rolsuper AND rolname NOT IN('photo_fn_owner','photo_scheduler') LOOP
   IF has_function_privilege(role_row.oid,f,'EXECUTE') AND NOT pg_has_role(role_row.oid,'photo_scheduler','USAGE') AND NOT pg_has_role(role_row.oid,'photo_fn_owner','USAGE') THEN RAISE EXCEPTION 'APPLY_LOCATOR_FOREIGN_EXECUTE' USING ERRCODE='55000';END IF;
  END LOOP;
 END LOOP;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_inputs(uuid,uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'b7e49c267c73eeb6efa19f83ba2fac50712ffbf9a558a36e06cba3caa8e4ec37' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_stage_outputs(uuid,uuid,uuid,text,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'e73d673abd2d22d742a6808f983698cdfd608192bfa2b690f9a962f0ef0ecb33' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_v4_inputs(uuid,uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '4fdbdf0eac9fe83a0f42c0d71174453700bcee3055e0a39bcf5507fdf5e92461' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_v4_inputs_with_metadata(uuid,uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'e19e3b5ef853cd25d5f74a508d2843e0efaf7aa85cb5808cc3cdaec10ba0b797' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.apply_v4_stage_outputs(uuid,uuid,uuid,text,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '05d9c3fb4cf4f4130668638caaf24a4719cc8d27a5a8f37dee2ac5e6c797b0f0' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.authored_render_base_adoption_context(uuid,uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '4444e5e5d45274f0714aa2e910a00168f8bb85d746576fd43b7f76d933cc990a' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.claim_apply_build_versioned()'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '9e1bb31419af1892f7d4ca54150199f5e6b23c263ee38332d903b3b025654248' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.renew_apply_build(uuid,uuid)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'c21c7f7a203e1bf0cbf6df4adafe466a50cfdf59e85e419462284152ab454b8c' THEN RAISE EXCEPTION 'APPLY_LOCATOR_PREIMAGE_FUNCTION' USING ERRCODE='55000';END IF;
END $post$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$BEGIN RAISE EXCEPTION 'Forward-only migration';END$$;
-- +goose StatementEnd
