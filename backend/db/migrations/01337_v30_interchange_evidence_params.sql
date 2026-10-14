-- +goose Up
-- Frozen proof is persisted only by the fenced completion function. No new lifecycle table.
-- +goose StatementBegin
ALTER TABLE photo.backend_documents ADD COLUMN v30_import_evidence jsonb;
ALTER TABLE photo.backend_documents ADD COLUMN v30_import_parsed_jcs text;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v30_parsed_shape CHECK
 (v30_import_parsed_jcs IS NULL OR (purpose='v30_interchange_import_evidence' AND octet_length(v30_import_parsed_jcs) BETWEEN 2 AND 8388608));
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v30_evidence_shape CHECK
 (v30_import_evidence IS NULL OR (purpose='v30_interchange_import_evidence' AND jsonb_typeof(v30_import_evidence)='object'));
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.complete_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text,uuid,text) RENAME TO complete_v30_interchange_import_document_pre_evidence_pin;
REVOKE ALL ON FUNCTION photo.complete_v30_interchange_import_document_pre_evidence_pin(uuid,uuid,text,uuid,text,text,text,uuid,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.complete_v30_interchange_import_document(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_purpose text,p_body text,p_bucket text,p_token uuid,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE accepted boolean;
BEGIN
 accepted:=photo.complete_v30_interchange_import_document_pre_evidence_pin(p_tenant,p_workflow,p_parsed,p_artifact,p_purpose,p_body,p_bucket,p_token,p_version);
 IF accepted IS DISTINCT FROM true THEN RETURN false;END IF;
 IF p_purpose='v30_interchange_import_evidence' THEN
  UPDATE photo.backend_documents SET v30_import_evidence=p_body::jsonb,v30_import_parsed_jcs=p_parsed WHERE tenant_id=p_tenant AND artifact_id=p_artifact
   AND purpose=p_purpose AND token=p_token AND state='ready' AND object_version=p_version
   AND sha256=encode(sha256(convert_to(p_body,'UTF8')),'hex');
  IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_EVIDENCE_PIN_REFUSED' USING ERRCODE='23514';END IF;
 END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v30_interchange_import_document(uuid,uuid,text,uuid,text,text,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.v30_interchange_import_document_scope(uuid,uuid,text,uuid,text,text) RENAME TO v30_interchange_import_document_scope_pre_evidence_params;
CREATE FUNCTION photo.v30_interchange_import_document_scope(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_purpose text,p_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;scope jsonb;sources jsonb;metadata jsonb;d photo.backend_documents%ROWTYPE;task uuid;
BEGIN
 IF p_purpose IS DISTINCT FROM 'task_params' THEN
  RETURN photo.v30_interchange_import_document_scope_pre_evidence_params(p_tenant,p_workflow,p_parsed,p_artifact,p_purpose,p_body);
 END IF;
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 body:=p_body::jsonb;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR NOT(body ? 'evidence') THEN RETURN NULL;END IF;
 scope:=photo.v30_interchange_import_document_scope_pre_evidence_params(p_tenant,p_workflow,p_parsed,p_artifact,p_purpose,(body-'evidence')::text);
 IF scope IS NULL THEN RETURN NULL;END IF;
 task:=photo.v30_uuid5(p_workflow,'v2.interchange.import:0');
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=(scope->>'run')::uuid AND owner_id=task
  AND purpose='v30_interchange_import_evidence' AND artifact_id=photo.v30_uuid5(task,'interchange:evidence:1') AND state='ready' FOR SHARE;
 IF d.id IS NULL OR d.v30_import_evidence IS NULL OR body->'evidence' IS DISTINCT FROM jsonb_build_object('artifact_id',d.artifact_id,'sha256',d.sha256)
 OR d.v30_import_evidence->'plan' IS DISTINCT FROM body->'plan' OR d.v30_import_evidence->'parsed' IS DISTINCT FROM body->'parsed' THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR metadata IS NULL OR d.v30_import_evidence->'sources' IS DISTINCT FROM sources
 OR d.v30_import_evidence->'metadata' IS DISTINCT FROM metadata THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=d.artifact_id AND sha256=d.sha256 AND bytes=d.bytes
 AND bucket=d.bucket AND object_version=d.object_version AND state='ready' AND verified_at IS NOT NULL
 AND object_version NOT IN('','null') AND media_type='application/json' AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN scope||jsonb_build_object('sha',encode(sha256(convert_to(p_body,'UTF8')),'hex'),'bytes',octet_length(p_body));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_import_document_scope(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_document_scope(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
