-- +goose Up
-- Current background certificate is read only after the complete prepared certificate gate.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_prepared_background_certificate(p_tenant uuid,p_workflow uuid,p_parsed text,p_entry uuid,p_binding jsonb,p_artifact uuid,p_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;rid uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v30_interchange_prepared_document(p_tenant,p_workflow,p_parsed,p_entry,p_binding,p_artifact,p_sha,'StyleBackgroundAsset');
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT run_id INTO rid FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 SELECT jsonb_build_object('background_occurrence',d.v30_import_prepared->'background_occurrence','media_type',d.v30_import_prepared->>'media_type') INTO result FROM photo.backend_documents d
 WHERE d.tenant_id=p_tenant AND d.run_id=rid AND d.owner_id=p_artifact AND d.artifact_id=p_artifact
 AND d.purpose='v30_import_prepared' AND d.state='ready' AND d.sha256=p_sha
 AND d.v30_import_prepared->>'contract'='StyleBackgroundAsset' AND d.v30_import_prepared->>'entry'=p_entry::text
 AND d.v30_import_prepared->'binding'=p_binding FOR SHARE;
 IF result IS NULL OR jsonb_typeof(result) IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 RETURN result;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_background_certificate(uuid,uuid,text,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_background_certificate(uuid,uuid,text,uuid,jsonb,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_prepared_background_certificate(uuid,uuid,text,uuid,jsonb,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
