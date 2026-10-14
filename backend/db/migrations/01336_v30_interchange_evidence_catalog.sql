-- +goose Up
-- +goose StatementBegin
ALTER FUNCTION photo.v30_interchange_import_document(uuid,uuid,text,uuid,text,text) RENAME TO v30_interchange_import_document_pre_evidence;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_document_pre_evidence(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.v30_interchange_import_document(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;metadata jsonb;w photo.v2_workflows%ROWTYPE;task uuid;
BEGIN
 IF p_contract IS DISTINCT FROM 'FrozenInterchangeImportEvidence' THEN
  RETURN QUERY SELECT * FROM photo.v30_interchange_import_document_pre_evidence(p_tenant,p_workflow,p_parsed,p_artifact,p_sha,p_contract);RETURN;
 END IF;
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL THEN RETURN;END IF;
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,p_workflow,p_parsed);
 IF metadata IS NULL THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 task:=photo.v30_uuid5(p_workflow,'v2.interchange.import:0');
 IF p_artifact IS DISTINCT FROM photo.v30_uuid5(task,'interchange:evidence:1') THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=w.run_id AND d.owner_id=task AND d.purpose='v30_interchange_import_evidence'
 AND d.artifact_id=p_artifact AND d.sha256=p_sha AND d.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
 AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version NOT IN('','null') AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF d,a;
END $$;
ALTER FUNCTION photo.v30_interchange_import_document(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_document(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_import_document(uuid,uuid,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
