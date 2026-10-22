-- +goose Up
-- Row-lock prerequisites only. Existing link rows stay fully immutable; staged
-- Apply node identity gains an explicit id-only immutable barrier before grant.
-- No producer authority, table UPDATE, runtime role or model quality change.
-- +goose StatementBegin
DO $$
DECLARE owner_oid oid;table_owner_oid oid;before_table text;before_columns jsonb;before_runtime jsonb;
 before_id jsonb;after_id jsonb;before_function jsonb;
BEGIN
 SELECT oid INTO STRICT owner_oid FROM pg_roles WHERE rolname='photo_fn_owner';
 IF EXISTS(SELECT FROM pg_roles WHERE oid=owner_oid AND rolcanlogin) THEN
  RAISE EXCEPTION 'V23_DETAIL_LINK_ROW_LOCK_OWNER_LOGIN';
 END IF;
 SELECT relowner INTO STRICT table_owner_oid FROM pg_class WHERE oid='photo.v23_portrait_detail_event_links'::regclass;
 IF NOT EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='photo' AND c.relname='v23_portrait_detail_event_links' AND c.relkind='r'
   AND c.relrowsecurity AND c.relforcerowsecurity)
 OR NOT EXISTS(SELECT FROM pg_policy p WHERE p.polrelid='photo.v23_portrait_detail_event_links'::regclass
  AND p.polname='trusted_functions' AND p.polcmd='*' AND p.polpermissive
  AND p.polroles=ARRAY[owner_oid] AND pg_get_expr(p.polqual,p.polrelid)='true'
  AND pg_get_expr(p.polwithcheck,p.polrelid)='true')
 OR NOT EXISTS(SELECT FROM pg_trigger t WHERE t.tgrelid='photo.v23_portrait_detail_event_links'::regclass
  AND t.tgname='v23_portrait_detail_event_links_immutable' AND t.tgenabled='O'
  AND t.tgfoid='photo.guard_v2_immutable()'::regprocedure AND t.tgtype=19
  AND NOT t.tgisinternal AND t.tgnargs=0 AND t.tgqual IS NULL AND t.tgattr=''::int2vector)
 OR NOT EXISTS(SELECT FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
  WHERE p.oid='photo.guard_v2_immutable()'::regprocedure
   AND owner.rolname='photo_fn_owner' AND NOT p.prosecdef
   AND p.prosrc=E'\nBEGIN\n RAISE EXCEPTION ''V2_IMMUTABLE'' USING ERRCODE=''55000'';\nEND ')
 OR EXISTS(SELECT FROM (VALUES
 ('photo.v23_portrait_detail_event_producer(uuid,uuid,uuid,uuid,uuid)','f131a3d9f7cd77b1e9fdfb1e795299ec68d50f3f6392472654ab3e151ae25064',true),
 ('photo.v23_portrait_detail_event_producer_evaluate_core(uuid,uuid,uuid,uuid,uuid)','ad6cc3b0a299b993cb71a8444f6140ac3fdca8cb6da04de11cb49b636e3a4f62',false)
 ) expected(signature,body_sha,scheduler_public)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
 LEFT JOIN pg_language l ON l.oid=p.prolang
 WHERE p.oid IS NULL OR p.proowner<>owner_oid OR NOT p.prosecdef OR p.prokind<>'f'
  OR NOT p.proretset OR p.prorettype<>'record'::regtype OR p.pronargs<>5
  OR p.proargtypes<>'2950 2950 2950 2950 2950'::oidvector
  OR p.proargnames[1:5] IS DISTINCT FROM ARRAY['p_tenant','p_project','p_actor','p_auth_session','p_event']::text[]
  OR pg_get_function_result(p.oid)<>'TABLE(asset_version_id uuid, source_id uuid, source_sha text, consent_id uuid, consent_epoch bigint, run_id uuid, task_id uuid, result_id uuid, attempt_id uuid, image_id uuid, image_sha text, params_id uuid, params_sha text, resolved_id uuid, resolved_sha text)'
  OR l.lanname<>'plpgsql' OR p.provolatile<>'v'
  OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, photo']
  OR encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')<>expected.body_sha
  OR EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
   WHERE acl.privilege_type<>'EXECUTE' OR acl.is_grantable
    OR acl.grantee NOT IN(owner_oid,CASE WHEN expected.scheduler_public THEN 'photo_scheduler'::regrole::oid ELSE owner_oid END))
  OR EXISTS(SELECT FROM pg_roles runtime WHERE runtime.rolname IN('photo_api','photo_worker','photo_gateway','photo_portal','photo_maintenance','photo_scheduler')
   AND has_function_privilege(runtime.oid,p.oid,'EXECUTE') IS DISTINCT FROM (expected.scheduler_public AND runtime.rolname='photo_scheduler')))
 OR EXISTS(SELECT FROM (VALUES
 ('photo.bind_v23_portrait_detail_event(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid)','33d25a6a61bf3119ce216699a98b3d0e16d73ceb5ebfbb07efcd98985489828f',9,'photo_api','bool',false),
 ('photo.v23_portrait_detail_event_link_source(uuid,uuid,uuid,uuid,uuid)','3565da110b53b30081207083c7447279f9a937dc0a3896f597ca6b6f662958a4',5,'photo_scheduler','record',true),
 ('photo.v23_portrait_detail_event_link_evaluate_source(uuid,uuid,uuid,uuid)','e064e58e2e6f24efa7c2233c0fe0009a6ddb04bb6f73d1771eba9d3898b405b9',4,NULL::text,'record',true),
 ('photo.v23_evaluate_detail_closure_current(uuid,uuid,uuid,uuid,uuid)','328f5bc7222fa106825ad8ed01abe2d04e07d4041892334d92355c12a4d5fbf4',5,'photo_scheduler','bool',false)
 ) expected(signature,body_sha,nargs,caller,rettype,returns_set)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
 WHERE p.oid IS NULL OR p.proowner<>owner_oid OR NOT p.prosecdef OR p.prokind<>'f'
 OR p.pronargs<>expected.nargs OR p.proretset IS DISTINCT FROM expected.returns_set
 OR p.prorettype<>to_regtype(expected.rettype) OR p.prolang<>(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 OR p.provolatile<>'v' OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, photo']
 OR p.proargtypes::text<>array_to_string(array_fill('2950'::text,ARRAY[expected.nargs]),' ')
 OR encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')<>expected.body_sha
 OR EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
 WHERE acl.privilege_type<>'EXECUTE' OR acl.is_grantable OR acl.grantee NOT IN(owner_oid,coalesce(to_regrole(expected.caller)::oid,owner_oid)))
 OR EXISTS(SELECT FROM pg_roles runtime WHERE runtime.rolname IN('photo_api','photo_worker','photo_gateway','photo_portal','photo_maintenance','photo_scheduler')
 AND has_function_privilege(runtime.oid,p.oid,'EXECUTE') IS DISTINCT FROM (runtime.rolname=coalesce(expected.caller,''))))
 OR NOT has_table_privilege('photo_fn_owner','photo.v23_portrait_detail_event_links','SELECT')
 OR NOT has_table_privilege('photo_fn_owner','photo.v23_portrait_detail_event_links','INSERT')
 OR NOT has_table_privilege('photo_fn_owner','photo.v23_portrait_detail_event_links','DELETE')
 OR EXISTS(SELECT FROM pg_attribute a WHERE a.attrelid='photo.v23_portrait_detail_event_links'::regclass
  AND a.attnum>0 AND NOT a.attisdropped AND a.attname<>'event_id'
  AND has_column_privilege(owner_oid,a.attrelid,a.attnum,'UPDATE'))
 OR has_table_privilege('photo_fn_owner','photo.v23_portrait_detail_event_links','UPDATE') THEN
  RAISE EXCEPTION 'V23_DETAIL_LINK_ROW_LOCK_SOURCE_SHAPE';
 END IF;
 SELECT relacl::text INTO before_table FROM pg_class WHERE oid='photo.v23_portrait_detail_event_links'::regclass;
 SELECT jsonb_agg(jsonb_build_array(attnum,attacl::text) ORDER BY attnum) INTO before_columns
 FROM pg_attribute WHERE attrelid='photo.v23_portrait_detail_event_links'::regclass AND attnum>0 AND NOT attisdropped AND attname<>'event_id';
 SELECT jsonb_agg(jsonb_build_array(r.rolname,a.attname,has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE')) ORDER BY r.rolname,a.attnum)
 INTO before_runtime FROM pg_roles r CROSS JOIN pg_attribute a
 WHERE r.rolname LIKE 'photo_%' AND r.oid<>owner_oid AND a.attrelid='photo.v23_portrait_detail_event_links'::regclass AND a.attnum>0 AND NOT a.attisdropped;
 SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.grantor,x.grantee,x.privilege_type,x.is_grantable),'[]') INTO before_id
 FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x
 WHERE a.attrelid='photo.v23_portrait_detail_event_links'::regclass AND a.attname='event_id'
 AND NOT(x.grantee=owner_oid AND x.privilege_type='UPDATE' AND NOT x.is_grantable AND x.grantor=table_owner_oid);
 SELECT jsonb_agg(to_jsonb(p) ORDER BY p.oid) INTO before_function FROM pg_proc p WHERE p.oid IN('photo.v23_portrait_detail_event_producer(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_producer_evaluate_core(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.bind_v23_portrait_detail_event(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_link_source(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_link_evaluate_source(uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_evaluate_detail_closure_current(uuid,uuid,uuid,uuid,uuid)'::regprocedure);
 GRANT UPDATE(event_id) ON photo.v23_portrait_detail_event_links TO photo_fn_owner;
 SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.grantor,x.grantee,x.privilege_type,x.is_grantable),'[]') INTO after_id
 FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x
 WHERE a.attrelid='photo.v23_portrait_detail_event_links'::regclass AND a.attname='event_id'
 AND NOT(x.grantee=owner_oid AND x.privilege_type='UPDATE' AND NOT x.is_grantable AND x.grantor=table_owner_oid);
 IF before_table IS DISTINCT FROM (SELECT relacl::text FROM pg_class WHERE oid='photo.v23_portrait_detail_event_links'::regclass)
 OR before_columns IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_array(attnum,attacl::text) ORDER BY attnum) FROM pg_attribute WHERE attrelid='photo.v23_portrait_detail_event_links'::regclass AND attnum>0 AND NOT attisdropped AND attname<>'event_id')
 OR before_id IS DISTINCT FROM after_id
 OR before_runtime IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_array(r.rolname,a.attname,has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE')) ORDER BY r.rolname,a.attnum) FROM pg_roles r CROSS JOIN pg_attribute a WHERE r.rolname LIKE 'photo_%' AND r.oid<>owner_oid AND a.attrelid='photo.v23_portrait_detail_event_links'::regclass AND a.attnum>0 AND NOT a.attisdropped)
 OR before_function IS DISTINCT FROM (SELECT jsonb_agg(to_jsonb(p) ORDER BY p.oid) FROM pg_proc p WHERE p.oid IN('photo.v23_portrait_detail_event_producer(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_producer_evaluate_core(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.bind_v23_portrait_detail_event(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_link_source(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_link_evaluate_source(uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_evaluate_detail_closure_current(uuid,uuid,uuid,uuid,uuid)'::regprocedure))
 OR has_table_privilege('photo_fn_owner','photo.v23_portrait_detail_event_links','UPDATE')
 OR NOT has_column_privilege('photo_fn_owner','photo.v23_portrait_detail_event_links','event_id','UPDATE') THEN
  RAISE EXCEPTION 'V23_DETAIL_LINK_ROW_LOCK_ACL_DRIFT';
 END IF;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE owner_oid oid;table_owner_oid oid;before_table text;before_columns jsonb;before_runtime jsonb;
 before_id jsonb;after_id jsonb;before_function jsonb;
BEGIN
 SELECT oid INTO STRICT owner_oid FROM pg_roles WHERE rolname='photo_fn_owner';
 IF EXISTS(SELECT FROM pg_roles WHERE oid=owner_oid AND rolcanlogin) THEN
  RAISE EXCEPTION 'V23_DETAIL_NODE_ROW_LOCK_OWNER_LOGIN';
 END IF;
 SELECT relowner INTO STRICT table_owner_oid FROM pg_class WHERE oid='photo.apply_nodes'::regclass;
 IF NOT EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='photo' AND c.relname='apply_nodes' AND c.relkind='r'
   AND c.relrowsecurity AND c.relforcerowsecurity)
 OR NOT EXISTS(SELECT FROM pg_policy p WHERE p.polrelid='photo.apply_nodes'::regclass
  AND p.polname='trusted_functions' AND p.polcmd='*' AND p.polpermissive
  AND p.polroles=ARRAY[owner_oid] AND pg_get_expr(p.polqual,p.polrelid)='true'
  AND pg_get_expr(p.polwithcheck,p.polrelid)='true')
 OR EXISTS(SELECT FROM pg_trigger t WHERE t.tgrelid='photo.apply_nodes'::regclass AND t.tgname='apply_nodes_identity_immutable' AND NOT(t.tgenabled='O' AND t.tgfoid='photo.guard_v2_immutable()'::regprocedure AND t.tgtype=19
  AND NOT t.tgisinternal AND t.tgnargs=0 AND t.tgqual IS NULL
  AND t.tgattr::text=(SELECT attnum::text FROM pg_attribute WHERE attrelid='photo.apply_nodes'::regclass AND attname='id')))
 OR NOT EXISTS(SELECT FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
  WHERE p.oid='photo.guard_v2_immutable()'::regprocedure
   AND owner.rolname='photo_fn_owner' AND NOT p.prosecdef
   AND p.prosrc=E'\nBEGIN\n RAISE EXCEPTION ''V2_IMMUTABLE'' USING ERRCODE=''55000'';\nEND ')
 OR EXISTS(SELECT FROM (VALUES
 ('photo.v23_portrait_detail_event_producer(uuid,uuid,uuid,uuid,uuid)','f131a3d9f7cd77b1e9fdfb1e795299ec68d50f3f6392472654ab3e151ae25064',true),
 ('photo.v23_portrait_detail_event_producer_evaluate_core(uuid,uuid,uuid,uuid,uuid)','ad6cc3b0a299b993cb71a8444f6140ac3fdca8cb6da04de11cb49b636e3a4f62',false)
 ) expected(signature,body_sha,scheduler_public)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
 LEFT JOIN pg_language l ON l.oid=p.prolang
 WHERE p.oid IS NULL OR p.proowner<>owner_oid OR NOT p.prosecdef OR p.prokind<>'f'
  OR NOT p.proretset OR p.prorettype<>'record'::regtype OR p.pronargs<>5
  OR p.proargtypes<>'2950 2950 2950 2950 2950'::oidvector
  OR p.proargnames[1:5] IS DISTINCT FROM ARRAY['p_tenant','p_project','p_actor','p_auth_session','p_event']::text[]
  OR pg_get_function_result(p.oid)<>'TABLE(asset_version_id uuid, source_id uuid, source_sha text, consent_id uuid, consent_epoch bigint, run_id uuid, task_id uuid, result_id uuid, attempt_id uuid, image_id uuid, image_sha text, params_id uuid, params_sha text, resolved_id uuid, resolved_sha text)'
  OR l.lanname<>'plpgsql' OR p.provolatile<>'v'
  OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, photo']
  OR encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')<>expected.body_sha
  OR EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
   WHERE acl.privilege_type<>'EXECUTE' OR acl.is_grantable
    OR acl.grantee NOT IN(owner_oid,CASE WHEN expected.scheduler_public THEN 'photo_scheduler'::regrole::oid ELSE owner_oid END))
  OR EXISTS(SELECT FROM pg_roles runtime WHERE runtime.rolname IN('photo_api','photo_worker','photo_gateway','photo_portal','photo_maintenance','photo_scheduler')
   AND has_function_privilege(runtime.oid,p.oid,'EXECUTE') IS DISTINCT FROM (expected.scheduler_public AND runtime.rolname='photo_scheduler')))
 OR EXISTS(SELECT FROM (VALUES
 ('photo.bind_v23_portrait_detail_event(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid)','33d25a6a61bf3119ce216699a98b3d0e16d73ceb5ebfbb07efcd98985489828f',9,'photo_api','bool',false),
 ('photo.v23_portrait_detail_event_link_source(uuid,uuid,uuid,uuid,uuid)','3565da110b53b30081207083c7447279f9a937dc0a3896f597ca6b6f662958a4',5,'photo_scheduler','record',true),
 ('photo.v23_portrait_detail_event_link_evaluate_source(uuid,uuid,uuid,uuid)','e064e58e2e6f24efa7c2233c0fe0009a6ddb04bb6f73d1771eba9d3898b405b9',4,NULL::text,'record',true),
 ('photo.v23_evaluate_detail_closure_current(uuid,uuid,uuid,uuid,uuid)','328f5bc7222fa106825ad8ed01abe2d04e07d4041892334d92355c12a4d5fbf4',5,'photo_scheduler','bool',false)
 ) expected(signature,body_sha,nargs,caller,rettype,returns_set)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature)
 WHERE p.oid IS NULL OR p.proowner<>owner_oid OR NOT p.prosecdef OR p.prokind<>'f'
 OR p.pronargs<>expected.nargs OR p.proretset IS DISTINCT FROM expected.returns_set
 OR p.prorettype<>to_regtype(expected.rettype) OR p.prolang<>(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 OR p.provolatile<>'v' OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, photo']
 OR p.proargtypes::text<>array_to_string(array_fill('2950'::text,ARRAY[expected.nargs]),' ')
 OR encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')<>expected.body_sha
 OR EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
 WHERE acl.privilege_type<>'EXECUTE' OR acl.is_grantable OR acl.grantee NOT IN(owner_oid,coalesce(to_regrole(expected.caller)::oid,owner_oid)))
 OR EXISTS(SELECT FROM pg_roles runtime WHERE runtime.rolname IN('photo_api','photo_worker','photo_gateway','photo_portal','photo_maintenance','photo_scheduler')
 AND has_function_privilege(runtime.oid,p.oid,'EXECUTE') IS DISTINCT FROM (runtime.rolname=coalesce(expected.caller,''))))
 OR NOT has_table_privilege('photo_fn_owner','photo.apply_nodes','SELECT')
 OR NOT has_table_privilege('photo_fn_owner','photo.apply_nodes','INSERT')
 OR NOT has_table_privilege('photo_fn_owner','photo.apply_nodes','DELETE')
 OR EXISTS(SELECT FROM pg_attribute a WHERE a.attrelid='photo.apply_nodes'::regclass
  AND a.attnum>0 AND NOT a.attisdropped AND a.attname<>'id'
  AND has_column_privilege(owner_oid,a.attrelid,a.attnum,'UPDATE'))
 OR has_table_privilege('photo_fn_owner','photo.apply_nodes','UPDATE') THEN
  RAISE EXCEPTION 'V23_DETAIL_NODE_ROW_LOCK_SOURCE_SHAPE';
 END IF;
 SELECT relacl::text INTO before_table FROM pg_class WHERE oid='photo.apply_nodes'::regclass;
 SELECT jsonb_agg(jsonb_build_array(attnum,attacl::text) ORDER BY attnum) INTO before_columns
 FROM pg_attribute WHERE attrelid='photo.apply_nodes'::regclass AND attnum>0 AND NOT attisdropped AND attname<>'id';
 SELECT jsonb_agg(jsonb_build_array(r.rolname,a.attname,has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE')) ORDER BY r.rolname,a.attnum)
 INTO before_runtime FROM pg_roles r CROSS JOIN pg_attribute a
 WHERE r.rolname LIKE 'photo_%' AND r.oid<>owner_oid AND a.attrelid='photo.apply_nodes'::regclass AND a.attnum>0 AND NOT a.attisdropped;
 SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.grantor,x.grantee,x.privilege_type,x.is_grantable),'[]') INTO before_id
 FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x
 WHERE a.attrelid='photo.apply_nodes'::regclass AND a.attname='id'
 AND NOT(x.grantee=owner_oid AND x.privilege_type='UPDATE' AND NOT x.is_grantable AND x.grantor=table_owner_oid);
 SELECT jsonb_agg(to_jsonb(p) ORDER BY p.oid) INTO before_function FROM pg_proc p WHERE p.oid IN('photo.v23_portrait_detail_event_producer(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_producer_evaluate_core(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.bind_v23_portrait_detail_event(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_link_source(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_link_evaluate_source(uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_evaluate_detail_closure_current(uuid,uuid,uuid,uuid,uuid)'::regprocedure);
 IF NOT EXISTS(SELECT FROM pg_trigger WHERE tgrelid='photo.apply_nodes'::regclass AND tgname='apply_nodes_identity_immutable') THEN
  CREATE TRIGGER apply_nodes_identity_immutable BEFORE UPDATE OF id ON photo.apply_nodes FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
 END IF;
 GRANT UPDATE(id) ON photo.apply_nodes TO photo_fn_owner;
 SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.grantor,x.grantee,x.privilege_type,x.is_grantable),'[]') INTO after_id
 FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x
 WHERE a.attrelid='photo.apply_nodes'::regclass AND a.attname='id'
 AND NOT(x.grantee=owner_oid AND x.privilege_type='UPDATE' AND NOT x.is_grantable AND x.grantor=table_owner_oid);
 IF before_table IS DISTINCT FROM (SELECT relacl::text FROM pg_class WHERE oid='photo.apply_nodes'::regclass)
 OR before_columns IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_array(attnum,attacl::text) ORDER BY attnum) FROM pg_attribute WHERE attrelid='photo.apply_nodes'::regclass AND attnum>0 AND NOT attisdropped AND attname<>'id')
 OR before_id IS DISTINCT FROM after_id
 OR before_runtime IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_array(r.rolname,a.attname,has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE')) ORDER BY r.rolname,a.attnum) FROM pg_roles r CROSS JOIN pg_attribute a WHERE r.rolname LIKE 'photo_%' AND r.oid<>owner_oid AND a.attrelid='photo.apply_nodes'::regclass AND a.attnum>0 AND NOT a.attisdropped)
 OR before_function IS DISTINCT FROM (SELECT jsonb_agg(to_jsonb(p) ORDER BY p.oid) FROM pg_proc p WHERE p.oid IN('photo.v23_portrait_detail_event_producer(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_producer_evaluate_core(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.bind_v23_portrait_detail_event(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_link_source(uuid,uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_portrait_detail_event_link_evaluate_source(uuid,uuid,uuid,uuid)'::regprocedure,'photo.v23_evaluate_detail_closure_current(uuid,uuid,uuid,uuid,uuid)'::regprocedure))
 OR has_table_privilege('photo_fn_owner','photo.apply_nodes','UPDATE')
 OR NOT EXISTS(SELECT FROM pg_trigger t WHERE t.tgrelid='photo.apply_nodes'::regclass AND t.tgname='apply_nodes_identity_immutable' AND t.tgenabled='O' AND t.tgfoid='photo.guard_v2_immutable()'::regprocedure AND t.tgtype=19
  AND NOT t.tgisinternal AND t.tgnargs=0 AND t.tgqual IS NULL
  AND t.tgattr::text=(SELECT attnum::text FROM pg_attribute WHERE attrelid='photo.apply_nodes'::regclass AND attname='id'))
 OR NOT has_column_privilege('photo_fn_owner','photo.apply_nodes','id','UPDATE') THEN
  RAISE EXCEPTION 'V23_DETAIL_NODE_ROW_LOCK_ACL_DRIFT';
 END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
