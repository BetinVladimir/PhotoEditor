-- +goose Up
-- Scoped JSON roots/canonical masters only. Imported bytes never gain typed authority from manifest labels.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_import_document(p_tenant uuid,p_workflow uuid,p_parsed text,
 p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;metadata jsonb;w photo.v2_workflows%ROWTYPE;task uuid;authorized boolean:=false;expected_purpose text;
BEGIN
 IF p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR p_contract IS NULL OR p_contract NOT IN('Params','InterchangePlan','ParsedInterchange','WorkingImage') THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL THEN RETURN;END IF;
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,p_workflow,p_parsed);
 IF metadata IS NULL THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 task:=photo.v30_uuid5(p_workflow,'v2.interchange.import:0');
 IF p_contract='ParsedInterchange' THEN
  authorized:=sources->'parsed'=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha);
 ELSIF p_contract='WorkingImage' THEN
  authorized:=EXISTS(SELECT 1 FROM jsonb_array_elements(sources->'entries') AS e(value)
   WHERE value->>'state'='adopted' AND value->'master'=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha));
 ELSE
  expected_purpose:=CASE p_contract WHEN 'Params' THEN 'task_params' ELSE 'v30_interchange_import_plan' END;
  authorized:=p_artifact=photo.v30_uuid5(task,CASE p_contract WHEN 'Params' THEN 'interchange:params:1' ELSE 'interchange:plan:1' END)
   AND EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
    WHERE d.tenant_id=p_tenant AND d.run_id=w.run_id AND d.owner_id=task AND d.purpose=expected_purpose AND d.state='ready'
    AND d.artifact_id=p_artifact AND d.sha256=p_sha AND a.sha256=d.sha256 AND a.bytes=d.bytes
    AND a.bucket=d.bucket AND a.object_version=d.object_version);
 END IF;
 IF authorized IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 8388608 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
END $$;
ALTER FUNCTION photo.v30_interchange_import_document(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_document(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_import_document(uuid,uuid,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
