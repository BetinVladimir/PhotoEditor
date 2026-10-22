-- +goose Up
-- Resolve PL/pgSQL record/table alias collisions in consent maintenance.
-- Preserve the installed function identity, authority, locking and settlement.
-- +goose StatementBegin
DO $migration$
DECLARE signature text; definition text; needle text; replacement text;
 original_oid oid; original_owner oid; original_acl aclitem[]; original_config text[];
 metadata record; fragment text;
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.withdraw_completed_glare_consent()',
  'photo.withdraw_completed_open_eyes_consent()'] LOOP
  SELECT p.oid,p.proowner,p.proacl,p.proconfig,p.prosecdef,p.prorettype,p.pronargs,l.lanname
   INTO metadata FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
   WHERE p.oid=signature::regprocedure;
  IF metadata.proowner IS DISTINCT FROM 'photo_fn_owner'::regrole OR NOT metadata.prosecdef
   OR metadata.prorettype IS DISTINCT FROM 'boolean'::regtype OR metadata.pronargs<>0
   OR metadata.lanname<>'plpgsql'
   OR NOT EXISTS(SELECT FROM unnest(metadata.proconfig) c WHERE replace(c,' ','')='search_path=pg_catalog,photo')
  THEN RAISE EXCEPTION 'CONSENT_WITHDRAWAL_INSTALLATION_DRIFT: %',signature;END IF;
  original_oid:=metadata.oid;original_owner:=metadata.proowner;
  original_acl:=metadata.proacl;original_config:=metadata.proconfig;
  definition:=pg_get_functiondef(original_oid);
  needle:=$source$ SELECT t.tenant_id,t.run_id,t.id,t.canonical_result_id INTO candidate FROM photo.tasks t JOIN photo.runs r
  ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.operation='portrait.glare' AND t.state='succeeded'
  AND r.status IN('running','waiting_review') AND NOT r.cancel_requested AND r.v28_glare_authority_failure IS NULL
  AND photo.completed_glare_consent_loss(t.tenant_id,t.id,t.canonical_result_id) IS NOT NULL ORDER BY r.created_at,r.id LIMIT 1;$source$;
  IF signature='photo.withdraw_completed_open_eyes_consent()' THEN
   needle:=replace(replace(replace(needle,'portrait.glare','portrait.open_eyes'),
    'v28_glare_authority_failure','v28_donor_authority_failure'),
    'completed_glare_consent_loss','completed_open_eyes_consent_loss');
  END IF;
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1
  THEN RAISE EXCEPTION 'CONSENT_WITHDRAWAL_DISCOVERY_SHAPE: %',signature;END IF;
  FOREACH fragment IN ARRAY ARRAY['FROM photo.tasks t JOIN photo.runs r',
   'SELECT t.tenant_id,t.run_id,t.id,t.canonical_result_id INTO candidate'] LOOP
   IF (length(needle)-length(replace(needle,fragment,'')))/length(fragment)<>1
   THEN RAISE EXCEPTION 'CONSENT_WITHDRAWAL_ALIAS_SHAPE: %',signature;END IF;
  END LOOP;
  replacement:=replace(replace(needle,'photo.tasks t JOIN photo.runs r',
   'photo.tasks candidate_task JOIN photo.runs candidate_run'),'t.','candidate_task.');
  replacement:=replace(replacement,'r.','candidate_run.');
  EXECUTE replace(definition,needle,replacement);
  SELECT p.oid,p.proowner,p.proacl,p.proconfig,p.prosecdef INTO metadata
   FROM pg_proc p WHERE p.oid=signature::regprocedure;
  IF metadata.oid IS DISTINCT FROM original_oid OR metadata.proowner IS DISTINCT FROM original_owner
   OR metadata.proacl IS DISTINCT FROM original_acl OR metadata.proconfig IS DISTINCT FROM original_config
   OR NOT metadata.prosecdef
  THEN RAISE EXCEPTION 'CONSENT_WITHDRAWAL_IDENTITY_CHANGED: %',signature;END IF;
 END LOOP;
END $migration$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
