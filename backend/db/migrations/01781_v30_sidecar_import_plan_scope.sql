-- +goose Up
-- Carry the parent parse proof's format into post-ingest source evidence and
-- require the import plan to repeat that exact format. Historical proofs
-- without a format remain PhotoEditor bundle imports.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_interchange_import_sources(uuid,uuid,text)'::regprocedure);
 needle:='session uuid;parsed jsonb;';replacement:='session uuid;source_format text;parsed jsonb;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import source variables';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='SELECT (d.v30_interchange_proof->>''auth_session_id'')::uuid INTO session FROM photo.backend_documents d';
 replacement:='SELECT (d.v30_interchange_proof->>''auth_session_id'')::uuid,coalesce(d.v30_interchange_proof->>''format'',''photoeditor_bundle'') INTO session,source_format FROM photo.backend_documents d';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import source proof read';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='AND d.purpose=''task_params'' AND d.state=''ready'';';
 replacement:='AND d.purpose=''task_params'' AND d.state=''ready'' AND d.v30_interchange_proof->>''project_id''=w.project_id::text;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import source proof scope';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='IF photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,session) IS DISTINCT FROM true';
 replacement:='IF coalesce(source_format,'''') NOT IN(''photoeditor_bundle'',''xmp'',''captureone_sidecar'') OR photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,session) IS DISTINCT FROM true';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import source authority';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='''ingest_run_id'',CASE WHEN n>0 THEN rid ELSE NULL END,''entries'',rows);';
 replacement:='''ingest_run_id'',CASE WHEN n>0 THEN rid ELSE NULL END,''entries'',rows) || CASE WHEN source_format=''photoeditor_bundle'' THEN ''{}''::jsonb ELSE jsonb_build_object(''format'',source_format) END;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import source result';END IF;
 EXECUTE replace(definition,needle,replacement);

 -- 01335 renamed the original plan validator; 01337 added a second wrapper.
 -- Both publication and materialization eventually call this original body.
 definition:=pg_get_functiondef('photo.v30_interchange_import_document_scope_pre_evidence(uuid,uuid,text,uuid,text,text)'::regprocedure);
 needle:='body->>''format'' IS DISTINCT FROM ''photoeditor_bundle''';
 replacement:='body->>''format'' IS DISTINCT FROM coalesce(sources->>''format'',''photoeditor_bundle'')';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import plan format gate';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
