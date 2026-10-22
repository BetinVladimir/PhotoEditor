-- +goose Up
ALTER TABLE photo.runs ADD COLUMN media_renderer_policy text;
UPDATE photo.runs SET media_renderer_policy='legacy_v1';
ALTER TABLE photo.runs ALTER COLUMN media_renderer_policy SET DEFAULT 'declared_encoding_alpha8_icc_v2';
ALTER TABLE photo.runs ALTER COLUMN media_renderer_policy SET NOT NULL;
ALTER TABLE photo.runs ADD CONSTRAINT runs_media_renderer_policy CHECK(media_renderer_policy IN('legacy_v1','declared_encoding_alpha8_icc_v2'));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_media_renderer_policy() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.media_renderer_policy IS DISTINCT FROM OLD.media_renderer_policy THEN
  RAISE EXCEPTION 'Immutable media renderer policy';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_media_renderer_policy() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_media_renderer_policy() FROM PUBLIC;
CREATE TRIGGER runs_media_renderer_policy_immutable BEFORE UPDATE OF media_renderer_policy ON photo.runs
FOR EACH ROW EXECUTE FUNCTION photo.guard_media_renderer_policy();
CREATE FUNCTION photo.media_renderer_policy_for_run(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE policy text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR NOT EXISTS(SELECT FROM photo.tenants WHERE id=p_tenant AND status='active') THEN
  RAISE EXCEPTION 'Media renderer policy scope unavailable';END IF;
 SELECT media_renderer_policy INTO policy FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT FOUND AND EXISTS(SELECT FROM photo.runs WHERE id=p_run) THEN
  RAISE EXCEPTION 'Media renderer policy scope unavailable';END IF;
 RETURN policy;
END
$$;
ALTER FUNCTION photo.media_renderer_policy_for_run(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.media_renderer_policy_for_run(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.media_renderer_policy_for_run(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- Retry jobs are durable commands before their deterministic run exists.
ALTER TABLE photo.media_retry_jobs ADD COLUMN media_renderer_policy text;
UPDATE photo.media_retry_jobs SET media_renderer_policy='legacy_v1';
ALTER TABLE photo.media_retry_jobs ALTER COLUMN media_renderer_policy SET DEFAULT 'declared_encoding_alpha8_icc_v2';
ALTER TABLE photo.media_retry_jobs ALTER COLUMN media_renderer_policy SET NOT NULL;
ALTER TABLE photo.media_retry_jobs ADD CONSTRAINT media_retry_jobs_renderer_policy CHECK(media_renderer_policy IN('legacy_v1','declared_encoding_alpha8_icc_v2'));
CREATE TRIGGER media_retry_jobs_renderer_policy_immutable BEFORE UPDATE OF media_renderer_policy ON photo.media_retry_jobs
FOR EACH ROW EXECUTE FUNCTION photo.guard_media_renderer_policy();
-- +goose StatementBegin
CREATE FUNCTION photo.media_renderer_policy_for_retry(p_tenant uuid,p_event uuid,p_token uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE policy text;
BEGIN
 SELECT j.media_renderer_policy INTO policy FROM photo.media_retry_jobs j
 JOIN photo.tenants t ON t.id=j.tenant_id AND t.status='active'

 WHERE j.tenant_id=p_tenant AND j.event_id=p_event AND j.status='materializing'
 AND j.materialize_token=p_token AND j.materialize_lease_until>clock_timestamp()
 AND ((j.origin='bulk_repair' AND EXISTS(SELECT FROM photo.bulk_operations b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 WHERE b.tenant_id=j.tenant_id AND b.id=j.bulk_operation_id AND NOT b.cancel_requested AND NOT r.cancel_requested))
 OR (j.origin IN('ingest','apply') AND EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=j.tenant_id AND r.id=j.origin_run_id AND r.kind=j.origin AND NOT r.cancel_requested)));
 IF NOT FOUND THEN RAISE EXCEPTION 'Media retry policy scope unavailable';END IF;
 RETURN policy;
END $$;
ALTER FUNCTION photo.media_renderer_policy_for_retry(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.media_renderer_policy_for_retry(uuid,uuid,uuid) FROM PUBLIC;
DO $$ DECLARE a record; BEGIN
 FOR a IN SELECT acl.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
 WHERE p.oid='photo.media_renderer_policy_for_retry(uuid,uuid,uuid)'::regprocedure AND acl.grantee<>0 AND acl.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION photo.media_renderer_policy_for_retry(uuid,uuid,uuid) FROM %I',pg_get_userbyid(a.grantee));
 END LOOP;
END $$;
GRANT EXECUTE ON FUNCTION photo.media_renderer_policy_for_retry(uuid,uuid,uuid) TO photo_scheduler;
-- Preserve the installed authority and immutable config shape; only inherit
-- the previously frozen job policy into the first run and its replay check.
DO $$ DECLARE definition text; revised text; BEGIN
 SELECT pg_get_functiondef(p.oid) INTO definition FROM pg_proc p
 WHERE p.oid='photo.begin_media_retry_plan(uuid,uuid,uuid,text,jsonb)'::regprocedure
 AND p.proowner='photo_fn_owner'::regrole AND p.prosecdef AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql');
 IF definition IS NULL THEN RAISE EXCEPTION 'Media retry begin authority missing';END IF;
 IF (length(definition)-length(replace(definition,'r.kind=''preview'' AND r.input_snapshot_id','')))/length('r.kind=''preview'' AND r.input_snapshot_id')<>1
 OR (length(definition)-length(replace(definition,'config_hash,created_by) VALUES(p_run,j.tenant_id,','')))/length('config_hash,created_by) VALUES(p_run,j.tenant_id,')<>1
 OR (length(definition)-length(replace(definition,'p_config,p_hash,creator);','')))/length('p_config,p_hash,creator);')<>1 THEN
 RAISE EXCEPTION 'Media retry begin policy anchor count changed';END IF;
 revised:=replace(definition,'r.kind=''preview'' AND r.input_snapshot_id','r.media_renderer_policy=j.media_renderer_policy AND r.kind=''preview'' AND r.input_snapshot_id');
 revised:=replace(revised,'config_hash,created_by) VALUES(p_run,j.tenant_id,','config_hash,created_by,media_renderer_policy) VALUES(p_run,j.tenant_id,');
 revised:=replace(revised,'p_config,p_hash,creator);','p_config,p_hash,creator,j.media_renderer_policy);');
 IF revised=definition OR strpos(revised,'r.media_renderer_policy=j.media_renderer_policy')=0
 OR strpos(revised,'p_config,p_hash,creator,j.media_renderer_policy);')=0
 OR strpos(revised,'config_hash,created_by,media_renderer_policy) VALUES')=0 THEN
 RAISE EXCEPTION 'Media retry begin policy anchors changed';END IF;
 EXECUTE revised;
END $$;
-- +goose StatementEnd
-- Preview discovery may precede the first child run. Its producer already
-- belongs to a durable run; never treat this recovery as a new user command.
-- +goose StatementBegin
CREATE FUNCTION photo.media_renderer_policy_for_preview(p_tenant uuid,p_child uuid,p_producer uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent photo.runs%ROWTYPE; child photo.runs%ROWTYPE;
BEGIN
 SELECT r.* INTO parent FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 WHERE t.tenant_id=p_tenant AND t.id=p_producer AND NOT t.cancel_requested AND NOT r.cancel_requested;
 IF NOT FOUND OR p_child IS NULL THEN RAISE EXCEPTION 'Media preview policy source unavailable';END IF;
 SELECT * INTO child FROM photo.runs WHERE id=p_child;
 IF FOUND THEN
  IF child.tenant_id IS DISTINCT FROM p_tenant OR child.kind<>'preview' OR child.cancel_requested
   OR child.config_snapshot->>'source_task_id' IS DISTINCT FROM p_producer::text THEN
   RAISE EXCEPTION 'Media preview policy intent unavailable';END IF;
  RETURN child.media_renderer_policy;
 END IF;
 RETURN parent.media_renderer_policy;
END $$;
ALTER FUNCTION photo.media_renderer_policy_for_preview(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.media_renderer_policy_for_preview(uuid,uuid,uuid) FROM PUBLIC;
DO $$ DECLARE a record; BEGIN
 FOR a IN SELECT acl.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
 WHERE p.oid='photo.media_renderer_policy_for_preview(uuid,uuid,uuid)'::regprocedure AND acl.grantee<>0 AND acl.grantee<>p.proowner LOOP
 EXECUTE format('REVOKE ALL ON FUNCTION photo.media_renderer_policy_for_preview(uuid,uuid,uuid) FROM %I',pg_get_userbyid(a.grantee));END LOOP;
END $$;
GRANT EXECUTE ON FUNCTION photo.media_renderer_policy_for_preview(uuid,uuid,uuid) TO photo_scheduler;
DO $$ DECLARE item record; definition text; revised text; total integer:=0; BEGIN
 FOR item IN SELECT * FROM (VALUES
 ('save_v25_lens_preview_intent(uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)','v25_lens_preview_task_id'),
 ('save_v29_perspective_preview_intent(uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)','v29_perspective_preview_task_id'),
 ('save_v29_multi_preview_intent(uuid,uuid,text,uuid,integer,bigint,uuid,text,text,uuid)','v29_multi_preview_task_id'),
 ('save_v28_restoration_preview_intent(uuid,uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)','v28_restoration_preview_task_id')) entries(signature,pin) LOOP
 SELECT pg_get_functiondef(p.oid) INTO definition FROM pg_proc p
 WHERE p.oid=to_regprocedure('photo.'||item.signature) AND p.proowner='photo_fn_owner'::regrole AND p.prosecdef AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql');
 IF definition IS NULL THEN RAISE EXCEPTION 'Media preview save authority missing: %',item.signature;END IF;
 IF (length(definition)-length(replace(definition,item.pin||')','')))/length(item.pin||')')<>1
 OR (length(definition)-length(replace(definition,'source.actor_id,false,p_task);','')))/length('source.actor_id,false,p_task);')<>1
 OR (length(definition)-length(replace(definition,'old.config_snapshot=body AND NOT old.cancel_requested','')))/length('old.config_snapshot=body AND NOT old.cancel_requested')<>1 THEN
 RAISE EXCEPTION 'Media preview save policy anchor count changed: %',item.signature;END IF;
 revised:=replace(definition,item.pin||')',item.pin||',media_renderer_policy)');
 revised:=replace(revised,'source.actor_id,false,p_task);','source.actor_id,false,p_task,parent.media_renderer_policy);');
 -- Full canonical body equality and live source/attempt/fence checks remain
 -- unchanged. Replay additionally cannot silently change its policy.
 revised:=replace(revised,'old.config_snapshot=body AND NOT old.cancel_requested','old.config_snapshot=body AND old.media_renderer_policy=parent.media_renderer_policy AND NOT old.cancel_requested');
 IF revised=definition OR strpos(revised,'source.actor_id,false,p_task,parent.media_renderer_policy);')=0
 OR strpos(revised,item.pin||',media_renderer_policy)')=0
 OR strpos(revised,'old.media_renderer_policy=parent.media_renderer_policy')=0 THEN
 RAISE EXCEPTION 'Media preview save policy anchors changed: %',item.signature;END IF;
 EXECUTE revised;total:=total+1;
 END LOOP;
 IF total<>4 THEN RAISE EXCEPTION 'Media preview save policy inventory changed';END IF;
END $$;
-- +goose StatementEnd
-- Keep frozen v1 Params/restores executable; fresh Go compilers/cache readers use
-- renderer_policy v2. Both branches retain the complete installed authority
-- body. No artifacts, receipts, immutable Params or historical hashes change.
-- +goose StatementBegin
DO $migration$
DECLARE item record; definition text; revised text; wrapper text; args text;
 old_name text; new_name text; calls text; total integer:=0; anchor text; grant_row record;
BEGIN
 anchor:='"quality":88,"schema_version":1';
 FOR item IN
  SELECT p.oid,p.proname,p.prosrc,p.prosecdef,p.proargnames,p.proargtypes,p.proowner,
   p.prorettype,l.lanname
  FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
   JOIN pg_language l ON l.oid=p.prolang
  WHERE n.nspname='photo' AND p.oid=ANY(ARRAY[
   to_regprocedure('photo.pin_v26_render_derivative_document(uuid,uuid,text,text,text,text,text)'),
   to_regprocedure('photo.pin_v29_perspective_preview_document(uuid,uuid,text,text,text,text,text)'),
   to_regprocedure('photo.pin_v29_multi_preview_document(uuid,uuid,text,text,text,text,text)'),
   to_regprocedure('photo.pin_v28_restoration_preview_document(uuid,uuid,text,text,text,text,text)'),
   to_regprocedure('photo.pin_v25_lens_preview_document(uuid,uuid,text,text,text,text,text)'),
   to_regprocedure('photo.v28_open_eyes_base_preview_ready(uuid,uuid,text)'),
   to_regprocedure('photo.v28_relight_base_preview_ready(uuid,uuid,text)'),
   to_regprocedure('photo.v28_local_portrait_base_preview_ready(uuid,uuid,text)'),
   to_regprocedure('photo.review_after_media_live(uuid,uuid,uuid,text,text)'),
   to_regprocedure('photo.v28_local_portrait_review_preview_ready(uuid,uuid)'),
   to_regprocedure('photo.v28_relight_viewed_pair_ready(uuid,uuid,uuid,uuid,bigint,jsonb)')]) ORDER BY p.proname
 LOOP
  total:=total+1;
  IF item.proowner<>'photo_fn_owner'::regrole OR NOT item.prosecdef OR item.lanname NOT IN('sql','plpgsql')
   OR item.prorettype NOT IN('boolean'::regtype,'uuid'::regtype)
   OR item.proargnames IS NULL
   OR (length(item.prosrc)-length(replace(item.prosrc,anchor,'')))/length(anchor)<>1
  THEN RAISE EXCEPTION 'Unexpected media profile gate %',item.proname;END IF;
  definition:=pg_get_functiondef(item.oid);
  old_name:=item.proname||'_media_v1';new_name:=item.proname||'_media_v2';
  IF EXISTS(SELECT FROM pg_proc p WHERE p.pronamespace='photo'::regnamespace
    AND p.proname IN(old_name,new_name)) THEN
   RAISE EXCEPTION 'Media policy branches already exist for %',item.proname;END IF;
  -- CREATE OR REPLACE keeps public function OIDs and their existing ACLs. The
  -- private branches have no caller grants and run only through that boundary.
  revised:=replace(definition,'FUNCTION photo.'||item.proname||'(',
    'FUNCTION photo.'||old_name||'(');
  IF revised=definition THEN RAISE EXCEPTION 'Missing media signature';END IF;
  EXECUTE revised;
  EXECUTE format('ALTER FUNCTION photo.%I(%s) OWNER TO photo_fn_owner',old_name,pg_get_function_identity_arguments(item.oid));
  EXECUTE format('REVOKE ALL ON FUNCTION photo.%I(%s) FROM PUBLIC',old_name,pg_get_function_identity_arguments(item.oid));
  revised:=replace(definition,'FUNCTION photo.'||item.proname||'(',
    'FUNCTION photo.'||new_name||'(');
  revised:=replace(revised,anchor,'"quality":88,"renderer_policy":"declared_encoding_alpha8_icc_v2","schema_version":1');
  EXECUTE revised;
  EXECUTE format('ALTER FUNCTION photo.%I(%s) OWNER TO photo_fn_owner',new_name,pg_get_function_identity_arguments(item.oid));
  EXECUTE format('REVOKE ALL ON FUNCTION photo.%I(%s) FROM PUBLIC',new_name,pg_get_function_identity_arguments(item.oid));
  FOR grant_row IN SELECT p.proname,a.grantee FROM pg_proc p
    CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
    WHERE p.pronamespace='photo'::regnamespace AND p.proname IN(old_name,new_name)
      AND a.grantee<>0 AND a.grantee<>p.proowner
  LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION photo.%I(%s) FROM %I',
    grant_row.proname,pg_get_function_identity_arguments(item.oid),pg_get_userbyid(grant_row.grantee));
  END LOOP;
  SELECT string_agg(quote_ident(name),',' ORDER BY ordinal) INTO args
   FROM unnest(item.proargnames) WITH ORDINALITY names(name,ordinal);
  calls:=format('photo.%I(%s)',new_name,args);
  IF item.prorettype='uuid'::regtype THEN
   -- Frozen run policy selects exactly one writer; fresh runs cannot publish
   -- legacy Params even though historical immutable documents remain replayable.
   calls:=format('(CASE WHEN photo.media_renderer_policy_for_run(p_tenant,p_run)=''legacy_v1'' THEN photo.%I(%s) ELSE %s END)',old_name,args,calls);
  ELSE
   calls:=format('(coalesce(%s,false) OR coalesce(photo.%I(%s),false))',calls,old_name,args);
  END IF;
  wrapper:=CASE item.lanname WHEN 'sql' THEN 'SELECT '||calls
   ELSE 'BEGIN RETURN '||calls||'; END' END;
  EXECUTE replace(definition,item.prosrc,wrapper);
 END LOOP;
 IF total<>11 THEN RAISE EXCEPTION 'Expected 11 exact media policy gates, got %',total;END IF;
END $migration$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.media_renderer_policy_ready() RETURNS boolean
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,photo AS $$
 WITH names(name,args) AS (VALUES
 ('pin_v26_render_derivative_document','uuid,uuid,text,text,text,text,text'),
 ('pin_v29_perspective_preview_document','uuid,uuid,text,text,text,text,text'),
 ('pin_v29_multi_preview_document','uuid,uuid,text,text,text,text,text'),
 ('pin_v28_restoration_preview_document','uuid,uuid,text,text,text,text,text'),
 ('pin_v25_lens_preview_document','uuid,uuid,text,text,text,text,text'),
 ('v28_open_eyes_base_preview_ready','uuid,uuid,text'),
 ('v28_relight_base_preview_ready','uuid,uuid,text'),
 ('v28_local_portrait_base_preview_ready','uuid,uuid,text'),
 ('review_after_media_live','uuid,uuid,uuid,text,text'),
 ('v28_local_portrait_review_preview_ready','uuid,uuid'),
 ('v28_relight_viewed_pair_ready','uuid,uuid,uuid,uuid,bigint,jsonb')),
 expected AS (SELECT name,args,suffix,name||suffix AS full_name FROM names
 CROSS JOIN (VALUES(''),('_media_v1'),('_media_v2')) kinds(suffix))
 SELECT EXISTS(SELECT FROM pg_attribute a JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
  WHERE a.attrelid='photo.runs'::regclass AND a.attname='media_renderer_policy'
  AND a.attnotnull AND NOT a.attisdropped
  AND pg_get_expr(d.adbin,d.adrelid)='''declared_encoding_alpha8_icc_v2''::text')
 AND EXISTS(SELECT FROM pg_constraint c WHERE c.conrelid='photo.runs'::regclass
  AND c.conname='runs_media_renderer_policy' AND c.contype='c' AND c.convalidated
  AND regexp_replace(pg_get_constraintdef(c.oid),'[[:space:]]','','g')=
   'CHECK((media_renderer_policy=ANY(ARRAY[''legacy_v1''::text,''declared_encoding_alpha8_icc_v2''::text])))')
 AND EXISTS(SELECT FROM pg_trigger t JOIN pg_proc g ON g.oid=t.tgfoid
  WHERE t.tgrelid='photo.runs'::regclass AND t.tgname='runs_media_renderer_policy_immutable'
  AND t.tgenabled='O' AND t.tgtype=19 AND t.tgqual IS NULL AND t.tgnargs=0
  AND t.tgattr::text=(SELECT attnum::text FROM pg_attribute WHERE attrelid='photo.runs'::regclass AND attname='media_renderer_policy' AND NOT attisdropped)
  AND t.tgfoid='photo.guard_media_renderer_policy()'::regprocedure
  AND NOT g.prosecdef AND g.proowner='photo_fn_owner'::regrole
  AND g.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
  AND EXISTS(SELECT FROM unnest(g.proconfig) value WHERE replace(value,' ','')='search_path=pg_catalog,photo')
  AND regexp_replace(g.prosrc,'[[:space:]]','','g')=regexp_replace($guard$
BEGIN
 IF NEW.media_renderer_policy IS DISTINCT FROM OLD.media_renderer_policy THEN
  RAISE EXCEPTION 'Immutable media renderer policy';END IF;
 RETURN NEW;
END $guard$,'[[:space:]]','','g'))
 AND EXISTS(SELECT FROM pg_proc reader WHERE reader.oid=to_regprocedure('photo.media_renderer_policy_for_run(uuid,uuid)')
  AND reader.proowner='photo_fn_owner'::regrole AND reader.prosecdef
  AND reader.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
  AND reader.prorettype='text'::regtype
  AND regexp_replace(reader.prosrc,'[[:space:]]','','g')=regexp_replace($reader$
DECLARE policy text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR NOT EXISTS(SELECT FROM photo.tenants WHERE id=p_tenant AND status='active') THEN
  RAISE EXCEPTION 'Media renderer policy scope unavailable';END IF;
 SELECT media_renderer_policy INTO policy FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT FOUND AND EXISTS(SELECT FROM photo.runs WHERE id=p_run) THEN
  RAISE EXCEPTION 'Media renderer policy scope unavailable';END IF;
 RETURN policy;
END $reader$,'[[:space:]]','','g')
  AND EXISTS(SELECT FROM unnest(reader.proconfig) value WHERE replace(value,' ','')='search_path=pg_catalog,photo')
  AND has_function_privilege('photo_scheduler',reader.oid,'EXECUTE')
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(reader.proacl,acldefault('f',reader.proowner))) a
   WHERE a.grantee NOT IN(reader.proowner,'photo_scheduler'::regrole)))
 AND EXISTS(SELECT FROM pg_attribute a JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
  WHERE a.attrelid='photo.media_retry_jobs'::regclass AND a.attname='media_renderer_policy' AND a.attnotnull AND NOT a.attisdropped
  AND pg_get_expr(d.adbin,d.adrelid)='''declared_encoding_alpha8_icc_v2''::text')
 AND EXISTS(SELECT FROM pg_constraint c WHERE c.conrelid='photo.media_retry_jobs'::regclass
  AND c.conname='media_retry_jobs_renderer_policy' AND c.convalidated AND c.contype='c'
  AND regexp_replace(pg_get_constraintdef(c.oid),'[[:space:]]','','g')='CHECK((media_renderer_policy=ANY(ARRAY[''legacy_v1''::text,''declared_encoding_alpha8_icc_v2''::text])))')
 AND EXISTS(SELECT FROM pg_trigger t WHERE t.tgrelid='photo.media_retry_jobs'::regclass
  AND t.tgname='media_retry_jobs_renderer_policy_immutable' AND t.tgenabled='O' AND t.tgtype=19
  AND t.tgqual IS NULL AND t.tgnargs=0 AND t.tgfoid='photo.guard_media_renderer_policy()'::regprocedure
  AND t.tgattr::text=(SELECT attnum::text FROM pg_attribute WHERE attrelid=t.tgrelid AND attname='media_renderer_policy' AND NOT attisdropped))
 AND EXISTS(SELECT FROM pg_proc reader WHERE reader.oid=to_regprocedure('photo.media_renderer_policy_for_retry(uuid,uuid,uuid)')
  AND reader.proowner='photo_fn_owner'::regrole AND reader.prosecdef AND reader.prorettype='text'::regtype
  AND reader.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
  AND EXISTS(SELECT FROM unnest(reader.proconfig) value WHERE replace(value,' ','')='search_path=pg_catalog,photo')
  AND regexp_replace(reader.prosrc,'[[:space:]]','','g')=regexp_replace($retryreader$DECLARE policy text;
BEGIN
 SELECT j.media_renderer_policy INTO policy FROM photo.media_retry_jobs j
 JOIN photo.tenants t ON t.id=j.tenant_id AND t.status='active'

 WHERE j.tenant_id=p_tenant AND j.event_id=p_event AND j.status='materializing'
 AND j.materialize_token=p_token AND j.materialize_lease_until>clock_timestamp()
 AND ((j.origin='bulk_repair' AND EXISTS(SELECT FROM photo.bulk_operations b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 WHERE b.tenant_id=j.tenant_id AND b.id=j.bulk_operation_id AND NOT b.cancel_requested AND NOT r.cancel_requested))
 OR (j.origin IN('ingest','apply') AND EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=j.tenant_id AND r.id=j.origin_run_id AND r.kind=j.origin AND NOT r.cancel_requested)));
 IF NOT FOUND THEN RAISE EXCEPTION 'Media retry policy scope unavailable';END IF;
 RETURN policy;
END$retryreader$,'[[:space:]]','','g')
  AND has_function_privilege('photo_scheduler',reader.oid,'EXECUTE')
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(reader.proacl,acldefault('f',reader.proowner))) a
   WHERE a.grantee NOT IN(reader.proowner,'photo_scheduler'::regrole)))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.begin_media_retry_plan(uuid,uuid,uuid,text,jsonb)')
  AND p.proowner='photo_fn_owner'::regrole AND p.prosecdef
  AND strpos(p.prosrc,'r.media_renderer_policy=j.media_renderer_policy AND r.kind=''preview''')>0
  AND strpos(p.prosrc,'config_hash,created_by,media_renderer_policy) VALUES')>0
  AND strpos(p.prosrc,'p_config,p_hash,creator,j.media_renderer_policy);')>0)
 AND EXISTS(SELECT FROM pg_proc reader WHERE reader.oid=to_regprocedure('photo.media_renderer_policy_for_preview(uuid,uuid,uuid)')
 AND reader.proowner='photo_fn_owner'::regrole AND reader.prosecdef AND reader.prorettype='text'::regtype
 AND reader.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND EXISTS(SELECT FROM unnest(reader.proconfig) value WHERE replace(value,' ','')='search_path=pg_catalog,photo')
 AND regexp_replace(reader.prosrc,'[[:space:]]','','g')=regexp_replace($previewreader$DECLARE parent photo.runs%ROWTYPE; child photo.runs%ROWTYPE;
BEGIN
 SELECT r.* INTO parent FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 WHERE t.tenant_id=p_tenant AND t.id=p_producer AND NOT t.cancel_requested AND NOT r.cancel_requested;
 IF NOT FOUND OR p_child IS NULL THEN RAISE EXCEPTION 'Media preview policy source unavailable';END IF;
 SELECT * INTO child FROM photo.runs WHERE id=p_child;
 IF FOUND THEN
  IF child.tenant_id IS DISTINCT FROM p_tenant OR child.kind<>'preview' OR child.cancel_requested
   OR child.config_snapshot->>'source_task_id' IS DISTINCT FROM p_producer::text THEN
   RAISE EXCEPTION 'Media preview policy intent unavailable';END IF;
  RETURN child.media_renderer_policy;
 END IF;
 RETURN parent.media_renderer_policy;
END$previewreader$,'[[:space:]]','','g')
 AND has_function_privilege('photo_scheduler',reader.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(reader.proacl,acldefault('f',reader.proowner))) a WHERE a.grantee NOT IN(reader.proowner,'photo_scheduler'::regrole)))
 AND (SELECT count(*)=4 AND bool_and(p.proowner='photo_fn_owner'::regrole AND p.prosecdef
 AND strpos(p.prosrc,'source.actor_id,false,p_task,parent.media_renderer_policy);')>0
 AND strpos(p.prosrc,'old.media_renderer_policy=parent.media_renderer_policy')>0
 AND strpos(p.prosrc,'old.config_snapshot=body')>0)
 FROM unnest(ARRAY[
 'photo.save_v25_lens_preview_intent(uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)',
 'photo.save_v29_perspective_preview_intent(uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)',
 'photo.save_v29_multi_preview_intent(uuid,uuid,text,uuid,integer,bigint,uuid,text,text,uuid)',
 'photo.save_v28_restoration_preview_intent(uuid,uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)']) signature
 JOIN pg_proc p ON p.oid=to_regprocedure(signature))
 AND count(*)=33 AND coalesce(bool_and(
  p.proowner='photo_fn_owner'::regrole AND p.prosecdef
  AND EXISTS(SELECT FROM unnest(p.proconfig) value WHERE replace(value,' ','')='search_path=pg_catalog,photo')
  AND CASE e.suffix
   WHEN '' THEN p.prosrc=((CASE l.lanname WHEN 'sql' THEN 'SELECT ' ELSE 'BEGIN RETURN ' END)
    ||CASE WHEN p.prorettype='uuid'::regtype THEN
     format('(CASE WHEN photo.media_renderer_policy_for_run(p_tenant,p_run)=''legacy_v1'' THEN photo.%I(%s) ELSE photo.%I(%s) END)',e.name||'_media_v1',a.args,e.name||'_media_v2',a.args)
    ELSE format('(coalesce(photo.%I(%s),false) OR coalesce(photo.%I(%s),false))',e.name||'_media_v2',a.args,e.name||'_media_v1',a.args) END
    ||CASE l.lanname WHEN 'sql' THEN '' ELSE '; END' END)
   WHEN '_media_v1' THEN strpos(p.prosrc,'"quality":88,"schema_version":1')>0
   ELSE strpos(p.prosrc,'"quality":88,"renderer_policy":"declared_encoding_alpha8_icc_v2","schema_version":1')>0 END
  AND (e.suffix='' OR NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee<>p.proowner))
 ),false)
 FROM expected e JOIN pg_proc p ON p.oid=to_regprocedure(format('photo.%I(%s)',e.full_name,e.args))
 JOIN pg_language l ON l.oid=p.prolang
 CROSS JOIN LATERAL(SELECT string_agg(quote_ident(name),',' ORDER BY ordinal) AS args
  FROM unnest(p.proargnames) WITH ORDINALITY names(name,ordinal)) a
$$;
ALTER FUNCTION photo.media_renderer_policy_ready() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.media_renderer_policy_ready() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.media_renderer_policy_ready() TO photo_scheduler,photo_worker;
-- +goose StatementEnd
-- Remove deployment default grants from both newly introduced readers too.
-- +goose StatementBegin
DO $$ DECLARE entry record; BEGIN
 FOR entry IN SELECT p.oid,p.proname,a.grantee FROM pg_proc p
 CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE p.pronamespace='photo'::regnamespace AND p.proname IN('media_renderer_policy_ready','media_renderer_policy_for_run','guard_media_renderer_policy')
 AND a.grantee<>0 AND a.grantee<>p.proowner LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',entry.oid::regprocedure,pg_get_userbyid(entry.grantee));
 END LOOP;
END $$;
GRANT EXECUTE ON FUNCTION photo.media_renderer_policy_for_run(uuid,uuid) TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.media_renderer_policy_ready() TO photo_scheduler,photo_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
