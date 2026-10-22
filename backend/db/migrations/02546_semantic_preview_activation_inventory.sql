-- +goose Up
-- The task-producing preview operation is distinct from the source evaluator.
-- Preserve every installed operation-specific predicate, wrapper, OID and ACL.
-- +goose StatementBegin
DO $$
DECLARE entry record; definition text; source text; revised text; total integer:=0;
 anchor constant text:='''semantic.local_exposure'',''optical.perspective_lines''';
 replacement constant text:='''semantic.local_exposure'',''semantic.preview_local_exposure'',''optical.perspective_lines''';
BEGIN
 FOR entry IN SELECT * FROM (VALUES
 ('photo.tenant_capabilities()'),
 ('photo.claim_task(uuid,integer,uuid,jsonb)'),
 ('photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)')) expected(signature) LOOP
 SELECT p.prosrc,pg_get_functiondef(p.oid) INTO source,definition FROM pg_proc p
 WHERE p.oid=to_regprocedure(entry.signature) AND p.prosecdef
 AND p.proowner='photo_fn_owner'::regrole
 AND p.prolang IN(SELECT oid FROM pg_language WHERE lanname IN('sql','plpgsql'))
 AND EXISTS(SELECT FROM unnest(p.proconfig) value WHERE replace(value,' ','')='search_path=pg_catalog,photo');
 IF source IS NULL OR strpos(source,'''semantic.preview_local_exposure''')>0
 OR (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN
  RAISE EXCEPTION 'SEMANTIC_PREVIEW_ACTIVATION_SOURCE_SHAPE: %',entry.signature;END IF;
 revised:=replace(source,anchor,replacement);
 EXECUTE replace(definition,source,revised);total:=total+1;
 END LOOP;
 IF total<>3 THEN RAISE EXCEPTION 'SEMANTIC_PREVIEW_ACTIVATION_INVENTORY';END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
