-- +goose Up
-- The TABLE output run_id must never shadow the UPDATE target run_id.
-- Preserve claim predicates, lease duration, ordering, ownership and ACL exactly.
-- +goose StatementBegin
DO $migration$
DECLARE before record; after record; definition text;
 needle text:='UPDATE photo.style_import_builds SET state=''building'',lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval ''60 seconds'' WHERE run_id=b.run_id RETURNING * INTO b;';
 replacement text:='UPDATE photo.style_import_builds AS claimed SET state=''building'',lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval ''60 seconds'' WHERE claimed.run_id=b.run_id RETURNING claimed.* INTO b;';
 expected_source text;
BEGIN
 SELECT p.* INTO STRICT before FROM pg_proc p WHERE p.oid='photo.claim_style_import_build()'::regprocedure;
 IF encode(sha256(convert_to(before.prosrc,'UTF8')),'hex')<>'7f4aaa4acbe2c260a25c6ba6334e0288f86d990e2174e56166f446b62bb2d8b9'
 OR before.proowner<>'photo_fn_owner'::regrole OR NOT before.prosecdef
 OR before.prolang<>(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 OR ('search_path=pg_catalog, photo'=ANY(before.proconfig) OR 'search_path=pg_catalog,photo'=ANY(before.proconfig)) IS NOT TRUE
 THEN RAISE EXCEPTION 'STYLE_IMPORT_CLAIM_PREIMAGE_DRIFT';END IF;
 IF (length(before.prosrc)-length(replace(before.prosrc,needle,'')))/length(needle)<>1
 THEN RAISE EXCEPTION 'STYLE_IMPORT_CLAIM_ANCHOR_DRIFT';END IF;
 expected_source:=replace(before.prosrc,needle,replacement);
 definition:=pg_get_functiondef(before.oid);EXECUTE replace(definition,needle,replacement);
 SELECT p.* INTO STRICT after FROM pg_proc p WHERE p.oid=before.oid;
 IF (to_jsonb(after)-'prosrc') IS DISTINCT FROM (to_jsonb(before)-'prosrc') OR after.prosrc IS DISTINCT FROM expected_source
 THEN RAISE EXCEPTION 'STYLE_IMPORT_CLAIM_POSTIMAGE_DRIFT';END IF;
END $migration$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02647 is forward-only';END $$;
-- +goose StatementEnd
