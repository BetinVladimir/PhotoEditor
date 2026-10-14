-- +goose Up
-- Storage scope only. Params/closure activation must separately pin evidence.
-- +goose StatementBegin
DO $$ DECLARE expression text;BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint
 WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'MISSING_DOCUMENT_PURPOSE_CONSTRAINT';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v30_interchange_import_evidence'')',expression);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.v30_interchange_import_document_scope(uuid,uuid,text,uuid,text,text) RENAME TO v30_interchange_import_document_scope_pre_evidence;
CREATE FUNCTION photo.v30_interchange_import_document_scope(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_purpose text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;metadata jsonb;body jsonb;diagnostic jsonb;source jsonb;original jsonb;
 w photo.v2_workflows%ROWTYPE;d photo.backend_documents%ROWTYPE;task uuid;plan_id uuid;ordinal integer;previous integer:=-1;
BEGIN
 IF p_purpose IS DISTINCT FROM 'v30_interchange_import_evidence' THEN
  RETURN photo.v30_interchange_import_document_scope_pre_evidence(p_tenant,p_workflow,p_parsed,p_artifact,p_purpose,p_body);
 END IF;
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL THEN RETURN NULL;END IF;
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,p_workflow,p_parsed);
 IF metadata IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 task:=photo.v30_uuid5(p_workflow,'v2.interchange.import:0');plan_id:=photo.v30_uuid5(task,'interchange:plan:1');
 IF p_artifact IS DISTINCT FROM photo.v30_uuid5(task,'interchange:evidence:1') THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=w.run_id AND owner_id=task
 AND purpose='v30_interchange_import_plan' AND artifact_id=plan_id AND state='ready' FOR SHARE;
 IF d.id IS NULL THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=plan_id AND sha256=d.sha256 AND bytes=d.bytes
 AND bucket=d.bucket AND object_version=d.object_version AND object_version NOT IN('','null')
 AND state='ready' AND verified_at IS NOT NULL AND media_type='application/json'
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 body:=p_body::jsonb;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(body))<>8
 OR NOT(body ?& ARRAY['schema_version','workflow_id','project_id','plan','parsed','sources','metadata','recipe_diagnostics'])
 OR body->'schema_version' IS DISTINCT FROM '1'::jsonb OR body->>'workflow_id' IS DISTINCT FROM p_workflow::text
 OR body->>'project_id' IS DISTINCT FROM w.project_id::text OR body->'sources' IS DISTINCT FROM sources
 OR body->'metadata' IS DISTINCT FROM metadata OR body->'parsed' IS DISTINCT FROM sources->'parsed'
 OR body->'plan' IS DISTINCT FROM jsonb_build_object('artifact_id',plan_id,'sha256',d.sha256)
 OR jsonb_typeof(body->'recipe_diagnostics') IS DISTINCT FROM 'array'
 OR jsonb_array_length(body->'recipe_diagnostics')>5000 THEN RETURN NULL;END IF;
 -- Full plan-null-recipe diagnostic coverage is checked by the typed compiler
 -- and worker, since this storage gate does not load plan object bytes.
 FOR diagnostic IN SELECT value FROM jsonb_array_elements(body->'recipe_diagnostics') LOOP
  IF jsonb_typeof(diagnostic) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(diagnostic))<>2
   OR NOT(diagnostic ?& ARRAY['entry_id','reason']) OR jsonb_typeof(diagnostic->'reason') IS DISTINCT FROM 'string'
   OR length(diagnostic->>'reason') NOT BETWEEN 1 AND 200 THEN RETURN NULL;END IF;
  SELECT value INTO source FROM jsonb_array_elements(sources->'entries') WHERE value->'entry_id'=diagnostic->'entry_id';
  IF source IS NULL OR source->>'state'<>'adopted' THEN RETURN NULL;END IF;
  ordinal:=(source->>'ordinal')::integer;original:=p_parsed::jsonb->'manifest'->'entries'->ordinal;
  IF ordinal<=previous OR original->'recipe_path' IS NULL OR original->'recipe_path'='null'::jsonb THEN RETURN NULL;END IF;
  previous:=ordinal;
 END LOOP;
 RETURN jsonb_build_object('run',w.run_id,'owner',task,'sha',encode(sha256(convert_to(p_body,'UTF8')),'hex'),'bytes',octet_length(p_body));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_import_document_scope(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_document_scope(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- Existing reserve/complete call this helper under tenant/run quota locks.
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
