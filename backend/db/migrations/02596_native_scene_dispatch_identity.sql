-- +goose Up
-- Native scene/depth profiles already own model_lock_sha; retain all dispatch
-- authority, resource, worker and contradictory-dual-pin refusal predicates.
-- +goose StatementBegin
DO $patch$
DECLARE target oid := to_regprocedure('photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)');
 before_row jsonb; after_row jsonb; source text; definition text; expected_source text;
 needle text := $old$IF t.operation IN('mask.person','face.detect','face.landmarks','face.parse') AND t.operation_version='1'$old$;
 replacement text := $new$IF t.operation IN('mask.person','face.detect','face.landmarks','face.parse','scene.parse','depth.predict') AND t.operation_version='1'$new$;
BEGIN
 IF target IS NULL THEN RAISE EXCEPTION 'NATIVE_SCENE_DISPATCH_MISSING';END IF;
 SELECT to_jsonb(p),p.prosrc,pg_get_functiondef(p.oid) INTO before_row,source,definition
 FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
 WHERE p.oid=target AND p.prokind='f' AND p.prosecdef AND p.proretset
 AND p.provolatile='v' AND l.lanname='plpgsql' AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
 AND p.proargnames=ARRAY['p_task','p_node','p_ram','p_vram','p_scratch','p_event','p_correlation','p_trace','outcome','command']::text[]
 AND pg_get_function_result(p.oid)='TABLE(outcome text, command jsonb)';
 IF source IS NULL THEN RAISE EXCEPTION 'NATIVE_SCENE_DISPATCH_METADATA_DRIFT';END IF;
 -- This is a private predecessor, callable only by its trusted function owner.
 IF EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE p.oid=target AND (a.grantee<>p.proowner OR a.privilege_type<>'EXECUTE'))
 OR EXISTS(SELECT 1 FROM unnest(ARRAY['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal','photo_semantic_admission']) role_name
 WHERE has_function_privilege(role_name,target,'EXECUTE')) THEN
 RAISE EXCEPTION 'NATIVE_SCENE_DISPATCH_ACL_DRIFT';END IF;
 IF (length(source)-length(replace(source,needle,'')))/length(needle)<>1
 OR strpos(source,replacement)>0
 OR (length(source)-length(replace(source,$guard$model_lock:=t.execution_profile->>'model_lock_id';$guard$,'')))/length($guard$model_lock:=t.execution_profile->>'model_lock_id';$guard$)<>1
 OR strpos(source,$guard$AND t.execution_profile->>'renderer_version'='v2.2' THEN$guard$)=0
 OR strpos(source,$guard$IF model_lock IS NOT NULL AND model_lock IS DISTINCT FROM t.execution_profile->>'model_lock_sha' THEN$guard$)=0
 OR strpos(source,$guard$model_lock:=t.execution_profile->>'model_lock_sha';$guard$)=0
 OR strpos(source,$guard$IF model_lock IS NULL OR model_lock !~ '^[0-9a-f]{64}$' THEN RETURN QUERY SELECT 'unqualified_model',NULL::jsonb; RETURN; END IF;$guard$)=0
 THEN RAISE EXCEPTION 'NATIVE_SCENE_DISPATCH_SOURCE_DRIFT';END IF;
 expected_source:=replace(source,needle,replacement);
 -- pg_get_functiondef retains the actual installed signature and configuration.
 EXECUTE replace(definition,source,expected_source);
 SELECT to_jsonb(p) INTO after_row FROM pg_proc p WHERE p.oid=target;
 IF after_row-'prosrc' IS DISTINCT FROM before_row-'prosrc'
 OR after_row->>'prosrc' IS DISTINCT FROM expected_source
 OR to_regprocedure('photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)') IS DISTINCT FROM target
 THEN RAISE EXCEPTION 'NATIVE_SCENE_DISPATCH_POSTCHECK';END IF;
END $patch$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
