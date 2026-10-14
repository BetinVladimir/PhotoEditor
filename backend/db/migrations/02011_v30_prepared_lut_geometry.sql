-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_prepared_lut_geometry(p_tenant uuid,p_workflow uuid,p_parsed text,p_entry uuid,p_binding jsonb,
 p_parent uuid,p_parent_sha text,p_artifact uuid,p_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;parent_doc photo.backend_documents%ROWTYPE;lut_doc photo.backend_documents%ROWTYPE;occ jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_parent IS NULL OR p_artifact IS NULL THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v30_interchange_prepared_document(p_tenant,p_workflow,p_parsed,p_entry,p_binding,p_parent,p_parent_sha,'ResolvedStyle');
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v30_interchange_prepared_document(p_tenant,p_workflow,p_parsed,p_entry,p_binding,p_artifact,p_sha,'AbsoluteLUTArray');
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT run_id INTO rid FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 SELECT * INTO parent_doc FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=rid AND owner_id=p_parent
 AND artifact_id=p_parent AND purpose='v30_import_prepared' AND state='ready' AND sha256=p_parent_sha FOR SHARE;
 IF NOT FOUND OR parent_doc.v30_import_prepared->>'contract' IS DISTINCT FROM 'ResolvedStyle' THEN RETURN NULL;END IF;
 SELECT * INTO lut_doc FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=rid AND owner_id=p_artifact
 AND artifact_id=p_artifact AND purpose='v30_import_prepared' AND state='ready' AND sha256=p_sha FOR SHARE;
 IF NOT FOUND OR lut_doc.v30_import_prepared->>'contract' IS DISTINCT FROM 'AbsoluteLUTArray' THEN RETURN NULL;END IF;
 occ:=lut_doc.v30_import_prepared->'lut_occurrence';
 IF jsonb_typeof(occ) IS DISTINCT FROM 'object' OR occ->'parent' IS DISTINCT FROM
 jsonb_build_object('artifact_id',parent_doc.v30_import_prepared#>>'{origin,artifact_id}',
 'sha256',parent_doc.v30_import_prepared#>>'{origin,sha256}')
 OR parent_doc.v30_import_prepared->>'entry' IS DISTINCT FROM p_entry::text
 OR parent_doc.v30_import_prepared->'binding' IS DISTINCT FROM p_binding
 OR lut_doc.v30_import_prepared->>'entry' IS DISTINCT FROM p_entry::text
 OR lut_doc.v30_import_prepared->'binding' IS DISTINCT FROM p_binding THEN RETURN NULL;END IF;
 RETURN occ;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_lut_geometry(uuid,uuid,text,uuid,jsonb,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_lut_geometry(uuid,uuid,text,uuid,jsonb,uuid,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_prepared_lut_geometry(uuid,uuid,text,uuid,jsonb,uuid,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
