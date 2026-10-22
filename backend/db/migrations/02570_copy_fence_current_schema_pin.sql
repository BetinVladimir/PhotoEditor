-- +goose Up
-- Current generated bundle adds lossless fence wire to exposure/background adoption.
-- Reader-first rollout keeps both historical schema generations and sealed digests.
-- Patch installed bodies, including renamed claim predecessors, preserving OID/ACL.
-- +goose StatementBegin
DO $rollout$
DECLARE
 old_pin constant text := '7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0';
 new_pin constant text := '75e3bcf5679402efe777b409f919bdcd594f92be5a163975ccf66e27dc3b6ee8';
 f record; ddl text; patched text; changed integer := 0;
 wire_seen boolean := false; claim_seen boolean := false; live_seen boolean := false;
 closure_seen boolean := false; preview_seal_seen boolean := false; apply_seal_seen boolean := false;
BEGIN
 FOR f IN SELECT p.oid,p.proname,p.proowner,p.proacl,pg_get_functiondef(p.oid) AS definition
 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND p.prokind='f' AND position(old_pin IN p.prosrc)>0
 ORDER BY p.oid LOOP
  ddl:=f.definition;
  IF f.proowner IS DISTINCT FROM 'photo_fn_owner'::regrole::oid THEN
   RAISE EXCEPTION 'COPY_FENCE_PIN_OWNER_DRIFT: %',f.proname;
  END IF;
  IF f.proname='shown_copy_schema_wire_ready' THEN
   IF position('p_schema='||quote_literal(old_pin) IN ddl)=0 THEN
    RAISE EXCEPTION 'COPY_FENCE_WIRE_SHAPE_DRIFT';
   END IF;
   patched:=replace(ddl,'p_schema='||quote_literal(old_pin),
    'p_schema IN ('||quote_literal(old_pin)||','||quote_literal(new_pin)||')');
   wire_seen:=true;
  ELSIF f.proname IN ('seal_semantic_retouch_preview_inputs','seal_semantic_shown_retouch_apply_inputs') THEN
   -- Only newly sealed profiles move forward; stored task rows are untouched.
   patched:=replace(ddl,quote_literal(old_pin),quote_literal(new_pin));
   preview_seal_seen:=preview_seal_seen OR f.proname='seal_semantic_retouch_preview_inputs';
   apply_seal_seen:=apply_seal_seen OR f.proname='seal_semantic_shown_retouch_apply_inputs';
  ELSIF f.proname IN ('semantic_shown_retouch_task_live','semantic_retouch_preview_closure_ready')
   OR position('SEMANTIC_SHOWN_RETOUCH_CLAIM_STALE' IN ddl)>0 THEN
   -- All these exact historical occurrences are final members of an IN list.
   IF position(quote_literal(old_pin)||')' IN ddl)=0 THEN
    RAISE EXCEPTION 'COPY_FENCE_READER_SHAPE_DRIFT: %',f.proname;
   END IF;
   patched:=replace(ddl,quote_literal(old_pin)||')',quote_literal(old_pin)||','||quote_literal(new_pin)||')');
   live_seen:=live_seen OR f.proname='semantic_shown_retouch_task_live';
   closure_seen:=closure_seen OR f.proname='semantic_retouch_preview_closure_ready';
   claim_seen:=claim_seen OR position('SEMANTIC_SHOWN_RETOUCH_CLAIM_STALE' IN ddl)>0;
  ELSE
   RAISE EXCEPTION 'COPY_FENCE_UNRECOGNIZED_PIN_CONSUMER: %',f.proname;
  END IF;
  IF patched=ddl THEN RAISE EXCEPTION 'COPY_FENCE_PIN_NOT_PATCHED: %',f.proname;END IF;
  EXECUTE patched;
  IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=f.oid
   AND p.proowner=f.proowner AND p.proacl IS NOT DISTINCT FROM f.proacl) THEN
   RAISE EXCEPTION 'COPY_FENCE_PIN_IDENTITY_DRIFT: %',f.proname;
  END IF;
  changed:=changed+1;
 END LOOP;
 IF changed<6 OR NOT (wire_seen AND claim_seen AND live_seen AND closure_seen
  AND preview_seal_seen AND apply_seal_seen) THEN
  RAISE EXCEPTION 'COPY_FENCE_PIN_INSTALLATION_INCOMPLETE';
 END IF;
END $rollout$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
