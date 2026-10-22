-- +goose Up
-- 02493 locks recorded dependency links FOR SHARE. PostgreSQL requires
-- UPDATE on one column even for an empty rowset. The existing unconditional
-- immutable trigger continues rejecting every actual UPDATE. Only the private
-- function owner receives the identity-column row-lock prerequisite.
-- +goose StatementBegin
DO $$
DECLARE owner_oid oid;table_owner_oid oid;before_table text;before_columns jsonb;before_runtime jsonb;
 before_id jsonb;after_id jsonb;before_function jsonb;
BEGIN
 SELECT oid INTO STRICT owner_oid FROM pg_roles WHERE rolname='photo_fn_owner';
 SELECT relowner INTO STRICT table_owner_oid FROM pg_class WHERE oid='photo.workflow_consent_links'::regclass;
 IF NOT EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='photo' AND c.relname='workflow_consent_links' AND c.relkind='r'
   AND c.relrowsecurity AND c.relforcerowsecurity)
 OR NOT EXISTS(SELECT FROM pg_policy p WHERE p.polrelid='photo.workflow_consent_links'::regclass
  AND p.polname='trusted_functions' AND p.polcmd='*' AND p.polpermissive
  AND p.polroles=ARRAY[owner_oid] AND pg_get_expr(p.polqual,p.polrelid)='true'
  AND pg_get_expr(p.polwithcheck,p.polrelid)='true')
 OR NOT EXISTS(SELECT FROM pg_trigger t WHERE t.tgrelid='photo.workflow_consent_links'::regclass
  AND t.tgname='workflow_consent_links_immutable' AND t.tgenabled='O'
  AND t.tgfoid='photo.guard_v2_immutable()'::regprocedure AND t.tgtype=19
  AND NOT t.tgisinternal AND t.tgnargs=0 AND t.tgqual IS NULL AND t.tgattr=''::int2vector)
 OR NOT EXISTS(SELECT FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
  WHERE p.oid='photo.guard_v2_immutable()'::regprocedure
   AND owner.rolname='photo_fn_owner' AND NOT p.prosecdef
   AND p.prosrc=E'\nBEGIN\n RAISE EXCEPTION ''V2_IMMUTABLE'' USING ERRCODE=''55000'';\nEND ')
 OR NOT EXISTS(SELECT FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
  WHERE p.oid='photo.v28_relight_recorded_subject_consents_live(uuid,jsonb)'::regprocedure
   AND p.proowner=owner_oid AND p.prosecdef AND p.prokind='f' AND NOT p.proretset
   AND p.prorettype='boolean'::regtype AND p.pronargs=2 AND p.proargtypes='2950 3802'::oidvector
   AND p.proargnames=ARRAY['p_tenant','p_subject'] AND l.lanname='plpgsql' AND p.provolatile='v'
   AND p.proconfig=ARRAY['search_path=pg_catalog, photo']
   AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')='68ac2247f338be8a95a069df64d554bf4660e7d999c80ce9ab7a982df33775c0'
   AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
    WHERE a.grantee<>owner_oid OR a.grantor<>owner_oid OR a.privilege_type<>'EXECUTE'))
 OR NOT has_table_privilege('photo_fn_owner','photo.consent_grants','SELECT')
 OR NOT has_table_privilege('photo_fn_owner','photo.consent_grants','UPDATE')
 OR has_table_privilege('photo_fn_owner','photo.workflow_consent_links','UPDATE') THEN
  RAISE EXCEPTION 'RELIGHT_CONSENT_LINK_ROW_LOCK_SOURCE_SHAPE';
 END IF;
 SELECT relacl::text INTO before_table FROM pg_class WHERE oid='photo.workflow_consent_links'::regclass;
 SELECT jsonb_agg(jsonb_build_array(attnum,attacl::text) ORDER BY attnum) INTO before_columns
 FROM pg_attribute WHERE attrelid='photo.workflow_consent_links'::regclass AND attnum>0 AND NOT attisdropped AND attname<>'id';
 SELECT jsonb_agg(jsonb_build_array(r.rolname,a.attname,has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE')) ORDER BY r.rolname,a.attnum)
 INTO before_runtime FROM pg_roles r CROSS JOIN pg_attribute a
 WHERE r.rolname LIKE 'photo_%' AND r.oid<>owner_oid AND a.attrelid='photo.workflow_consent_links'::regclass AND a.attnum>0 AND NOT a.attisdropped;
 SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.grantor,x.grantee,x.privilege_type,x.is_grantable),'[]') INTO before_id
 FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x
 WHERE a.attrelid='photo.workflow_consent_links'::regclass AND a.attname='id'
 AND NOT(x.grantee=owner_oid AND x.privilege_type='UPDATE' AND NOT x.is_grantable AND x.grantor=table_owner_oid);
 SELECT to_jsonb(p) INTO before_function FROM pg_proc p WHERE p.oid='photo.v28_relight_recorded_subject_consents_live(uuid,jsonb)'::regprocedure;
 GRANT UPDATE(id) ON photo.workflow_consent_links TO photo_fn_owner;
 SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.grantor,x.grantee,x.privilege_type,x.is_grantable),'[]') INTO after_id
 FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x
 WHERE a.attrelid='photo.workflow_consent_links'::regclass AND a.attname='id'
 AND NOT(x.grantee=owner_oid AND x.privilege_type='UPDATE' AND NOT x.is_grantable AND x.grantor=table_owner_oid);
 IF before_table IS DISTINCT FROM (SELECT relacl::text FROM pg_class WHERE oid='photo.workflow_consent_links'::regclass)
 OR before_columns IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_array(attnum,attacl::text) ORDER BY attnum) FROM pg_attribute WHERE attrelid='photo.workflow_consent_links'::regclass AND attnum>0 AND NOT attisdropped AND attname<>'id')
 OR before_id IS DISTINCT FROM after_id
 OR before_runtime IS DISTINCT FROM (SELECT jsonb_agg(jsonb_build_array(r.rolname,a.attname,has_column_privilege(r.oid,a.attrelid,a.attnum,'UPDATE')) ORDER BY r.rolname,a.attnum) FROM pg_roles r CROSS JOIN pg_attribute a WHERE r.rolname LIKE 'photo_%' AND r.oid<>owner_oid AND a.attrelid='photo.workflow_consent_links'::regclass AND a.attnum>0 AND NOT a.attisdropped)
 OR before_function IS DISTINCT FROM (SELECT to_jsonb(p) FROM pg_proc p WHERE p.oid='photo.v28_relight_recorded_subject_consents_live(uuid,jsonb)'::regprocedure)
 OR has_table_privilege('photo_fn_owner','photo.workflow_consent_links','UPDATE')
 OR NOT has_column_privilege('photo_fn_owner','photo.workflow_consent_links','id','UPDATE') THEN
  RAISE EXCEPTION 'RELIGHT_CONSENT_LINK_ROW_LOCK_ACL_DRIFT';
 END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
