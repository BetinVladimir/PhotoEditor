-- +goose Up
-- Preserve the installed compiler entrypoint, including queue-specific patches.
-- +goose StatementBegin
DO $$
DECLARE target oid:=to_regprocedure('photo.create_v2_draft_shell(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint)');
 source text;definition text;
 bad constant text:=$needle$coalesce(edge->>'prerequisite_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'$needle$;
 good constant text:=$needle$coalesce(edge->>'prerequisite_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'$needle$;
BEGIN
 SELECT p.prosrc,pg_get_functiondef(p.oid) INTO source,definition FROM pg_proc p
 WHERE p.oid=target AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql');
 IF source IS NULL OR (length(source)-length(replace(source,bad,'')))/length(bad)<>1
 OR strpos(source,'FOR edge IN SELECT value FROM jsonb_array_elements(p_edges) LOOP')=0
 OR strpos(source,'source_id:=(edge->>''prerequisite_id'')::uuid;')=0 THEN
  RAISE EXCEPTION 'V2_DRAFT_EDGE_UUID_BASE_SOURCE_SHAPE';
 END IF;
 EXECUTE replace(definition,source,replace(source,bad,good));
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
