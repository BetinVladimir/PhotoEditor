-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_prepared_defect_png_parents(p_tenant uuid,p_workflow uuid,p_parsed text,p_entry uuid,p_binding jsonb,p_artifact uuid,p_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;png photo.backend_documents%ROWTYPE;parent photo.backend_documents%ROWTYPE;occ jsonb;original jsonb;parents jsonb:='[]'::jsonb;matches integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_artifact IS NULL THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v30_interchange_prepared_document(p_tenant,p_workflow,p_parsed,p_entry,p_binding,p_artifact,p_sha,'DefectLabelPNG');
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT run_id INTO rid FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 SELECT * INTO png FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=rid AND owner_id=p_artifact AND artifact_id=p_artifact
 AND purpose='v30_import_prepared' AND state='ready' AND sha256=p_sha FOR SHARE;
 IF NOT FOUND OR png.v30_import_prepared->>'contract' IS DISTINCT FROM 'DefectLabelPNG'
 OR png.v30_import_prepared->>'entry' IS DISTINCT FROM p_entry::text OR png.v30_import_prepared->'binding' IS DISTINCT FROM p_binding THEN RETURN NULL;END IF;
 occ:=png.v30_import_prepared->'defect_png_occurrence';
 IF jsonb_typeof(occ->'reports') IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 IF jsonb_array_length(occ->'reports') NOT BETWEEN 1 AND 128 THEN RETURN NULL;END IF;
 FOR original IN SELECT value FROM jsonb_array_elements(occ->'reports') LOOP
  SELECT count(*) INTO matches FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=rid AND d.purpose='v30_import_prepared'
  AND d.state='ready' AND d.owner_id=d.artifact_id AND d.v30_import_prepared->>'contract'='DefectProposeReport'
  AND d.v30_import_prepared->>'entry'=p_entry::text AND d.v30_import_prepared->'binding'=p_binding
  AND d.v30_import_prepared#>>'{origin,artifact_id}'=original->>'artifact_id' AND d.v30_import_prepared#>>'{origin,sha256}'=original->>'sha256';
  IF matches<>1 THEN RETURN NULL;END IF;
  SELECT * INTO parent FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=rid AND d.purpose='v30_import_prepared'
  AND d.state='ready' AND d.owner_id=d.artifact_id AND d.v30_import_prepared->>'contract'='DefectProposeReport'
  AND d.v30_import_prepared->>'entry'=p_entry::text AND d.v30_import_prepared->'binding'=p_binding
  AND d.v30_import_prepared#>>'{origin,artifact_id}'=original->>'artifact_id' AND d.v30_import_prepared#>>'{origin,sha256}'=original->>'sha256' FOR SHARE;
  IF NOT FOUND THEN RETURN NULL;END IF;
  PERFORM 1 FROM photo.v30_interchange_prepared_document(p_tenant,p_workflow,p_parsed,p_entry,p_binding,parent.artifact_id,parent.sha256,'DefectProposeReport');
  IF NOT FOUND THEN RETURN NULL;END IF;
  parents:=parents||jsonb_build_array(jsonb_build_object('artifact_id',parent.artifact_id,'sha256',parent.sha256));
 END LOOP;
 RETURN parents;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_defect_png_parents(uuid,uuid,text,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_defect_png_parents(uuid,uuid,text,uuid,jsonb,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_prepared_defect_png_parents(uuid,uuid,text,uuid,jsonb,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
