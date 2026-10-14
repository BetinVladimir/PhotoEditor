-- +goose Up
-- Pin import source format in retained backend authority; historical proofs are bundle-only.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.save_v30_interchange_parse_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)'::regprocedure);
 needle:='proof:=jsonb_build_object(''schema_version'',1,''auth_session_id'',p_session,''project_id'',p_project,''bundle'',bundle);';
 replacement:='proof:=jsonb_build_object(''schema_version'',1,''auth_session_id'',p_session,''project_id'',p_project,''bundle'',bundle); IF request->>''format''<>''photoeditor_bundle'' THEN proof:=proof||jsonb_build_object(''format'',request->>''format''); END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import proof writer';END IF;
 EXECUTE replace(definition,needle,replacement);
 definition:=pg_get_functiondef('photo.v30_interchange_parse_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'::regprocedure);
 needle:='proof-''schema_version''-''auth_session_id''-''project_id''-''bundle''<>''{}''::jsonb';
 replacement:='proof-''schema_version''-''auth_session_id''-''project_id''-''bundle''-''format''<>''{}''::jsonb';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import proof fields';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='OR proof->''bundle'' IS DISTINCT FROM root';
 replacement:='OR proof->''bundle'' IS DISTINCT FROM root OR coalesce(proof->>''format'',''photoeditor_bundle'') IS DISTINCT FROM params->''request''->>''format''';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import proof binding';END IF;
 EXECUTE replace(definition,needle,replacement);
 definition:=pg_get_functiondef('photo.get_v30_interchange_pre_export(uuid,uuid,uuid,uuid)'::regprocedure);
 needle:='''direction'',''import'',''format'',''photoeditor_bundle''';
 replacement:=$patch$'direction','import','format',coalesce((SELECT d.v30_interchange_proof->>'format'
 FROM photo.tasks t JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.owner_id=t.id
 AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 WHERE t.tenant_id=w.tenant_id AND t.run_id=w.run_id AND t.operation='v2.interchange.parse'
 AND t.operation_version='1' AND d.v30_interchange_proof->>'project_id'=w.project_id::text
 ORDER BY t.id LIMIT 1),'photoeditor_bundle')$patch$;
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected import projection';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
