-- +goose Up
-- 02577's alias substitution also changed the operation string literal.
-- Repair only the two attested bodies; retain identity, ACL and every authority gate.
-- +goose StatementBegin
DO $migration$
DECLARE item record; before record; after record; definition text; bad text; good text; invalid_errors text; expected_source text;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('photo.withdraw_completed_glare_consent()', '04c03bfb980188aed3cbbf6fe52d796683763182e2636d7620a8af482b1115bd', 'glare'),
  ('photo.withdraw_completed_open_eyes_consent()', '4e707f702d7456006428246a0ad5ab5bef77fd94ff68e7318191c2007eb3f881', 'open_eyes')
 ) AS expected(signature,source_sha,suffix) LOOP
  SELECT p.oid,p.proowner,p.proacl,p.proconfig,p.prosecdef,p.prolang,p.prorettype,p.pronargs,p.prosrc
   INTO STRICT before FROM pg_proc p WHERE p.oid=item.signature::regprocedure;
  IF encode(sha256(convert_to(before.prosrc,'UTF8')),'hex')<>item.source_sha
   OR before.proowner<>'photo_fn_owner'::regrole OR NOT before.prosecdef
   OR before.prolang<>(SELECT oid FROM pg_language WHERE lanname='plpgsql')
   OR before.prorettype<>'boolean'::regtype OR before.pronargs<>0
   OR ('search_path=pg_catalog, photo'=ANY(before.proconfig)
           OR 'search_path=pg_catalog,photo'=ANY(before.proconfig)) IS NOT TRUE
  THEN RAISE EXCEPTION 'CONSENT_OPERATION_PREIMAGE_DRIFT: %',item.signature; END IF;
  bad:=quote_literal('portraicandidate_task.'||item.suffix);
  good:=quote_literal('portrait.'||item.suffix);
  IF (length(before.prosrc)-length(replace(before.prosrc,bad,'')))/length(bad)<>1
   OR position(good IN before.prosrc)>0
  THEN RAISE EXCEPTION 'CONSENT_OPERATION_LITERAL_SHAPE: %',item.signature; END IF;
  -- runs has no errors column. The existing immutable authority_failure and
  -- outbox code are the domain carriers; do not invent a generic error column.
  invalid_errors:=$source$,errors=errors||jsonb_build_array(jsonb_build_object('code',code,
  'message','The original consent or confirmed identity was withdrawn. Start a new request after authorization is restored.'))$source$;
  IF (length(before.prosrc)-length(replace(before.prosrc,invalid_errors,'')))/length(invalid_errors)<>1
   OR EXISTS(SELECT FROM pg_attribute WHERE attrelid='photo.runs'::regclass AND attname='errors' AND NOT attisdropped)
  THEN RAISE EXCEPTION 'CONSENT_OPERATION_ERROR_CARRIER_DRIFT: %',item.signature; END IF;
  expected_source:=replace(replace(before.prosrc,bad,good),invalid_errors,'');
  definition:=pg_get_functiondef(before.oid);
  EXECUTE replace(replace(definition,bad,good),invalid_errors,'');
  SELECT p.oid,p.proowner,p.proacl,p.proconfig,p.prosecdef,p.prolang,p.prorettype,p.pronargs,p.prosrc
   INTO STRICT after FROM pg_proc p WHERE p.oid=item.signature::regprocedure;
  IF (to_jsonb(after)-'prosrc') IS DISTINCT FROM (to_jsonb(before)-'prosrc')
   OR after.prosrc IS DISTINCT FROM expected_source
  THEN RAISE EXCEPTION 'CONSENT_OPERATION_POSTIMAGE_DRIFT: %',item.signature; END IF;
 END LOOP;
END $migration$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02643 is forward-only'; END $$;
-- +goose StatementEnd
