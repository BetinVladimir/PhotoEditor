-- +goose Up
-- Extend format predicates on current definitions, retaining ACL/fence/quota patches.
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;needle text;replacement text;
BEGIN
 FOR signature,needle,replacement IN SELECT * FROM (VALUES
 ('photo.v30_interchange_parse_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
  'params->''request''->>''format'' IS DISTINCT FROM ''photoeditor_bundle''',
  'coalesce(params->''request''->>''format'','''') NOT IN(''photoeditor_bundle'',''xmp'',''captureone_sidecar'')'),
 ('photo.save_v30_interchange_parse_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)',
  'request->>''format'' IS DISTINCT FROM ''photoeditor_bundle''',
  'coalesce(request->>''format'','''') NOT IN(''photoeditor_bundle'',''xmp'',''captureone_sidecar'')'),
 ('photo.begin_v30_interchange_parse_run(uuid,uuid,uuid,text,text,text,text,text)',
  'request->>''format'' IS DISTINCT FROM ''photoeditor_bundle''',
  'coalesce(request->>''format'','''') NOT IN(''photoeditor_bundle'',''xmp'',''captureone_sidecar'')'),
 ('photo.v30_interchange_document_scope(uuid,uuid,uuid,text,text,uuid,text,text)',
  'request->>''format'' IS DISTINCT FROM ''photoeditor_bundle''',
  'coalesce(request->>''format'','''') NOT IN(''photoeditor_bundle'',''xmp'',''captureone_sidecar'')')
 ) AS patches(signature,needle,replacement) LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'Unexpected sidecar parse format gate: %',signature;
  END IF;
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
