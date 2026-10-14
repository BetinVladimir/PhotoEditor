-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_approval_dependencies(p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_revision bigint,p_report_sha text,p_parsed text,p_entry uuid,p_binding jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;item record;entries jsonb:='[]'::jsonb;n integer:=0;matches integer;
BEGIN
 authority:=photo.v30_interchange_review_context(p_tenant,p_actor,p_session,p_workflow,p_revision,p_report_sha);
 IF authority IS NULL THEN RETURN NULL;END IF;
 FOR item IN SELECT d.artifact_id,d.sha256,d.v30_import_prepared->>'contract' AS contract
 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=(authority->>'run_id')::uuid
 AND d.purpose='v30_import_prepared' AND d.state='ready'
 AND d.v30_import_prepared->>'entry'=p_entry::text AND d.v30_import_prepared->'binding'=p_binding
 AND d.v30_import_prepared->>'contract' IN('StyleV2','CorrectionPlan','Override','LayerStack','MaskRef','Params')
 ORDER BY d.artifact_id LIMIT 4097 LOOP
  n:=n+1;IF n>4096 THEN RETURN NULL;END IF;
  SELECT count(*) INTO matches FROM photo.v30_interchange_approval_document(p_tenant,p_actor,p_session,p_workflow,p_revision,p_report_sha,p_parsed,p_entry,p_binding,item.artifact_id,item.sha256,item.contract);
  IF matches<>1 THEN RETURN NULL;END IF;
  entries:=entries||jsonb_build_array(jsonb_build_object('ref',jsonb_build_object('artifact_id',item.artifact_id,'sha256',item.sha256),'contract',item.contract));
 END LOOP;
 IF n=0 THEN RETURN NULL;END IF;
 RETURN entries;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_approval_dependencies(uuid,uuid,uuid,uuid,bigint,text,text,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_approval_dependencies(uuid,uuid,uuid,uuid,bigint,text,text,uuid,jsonb) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_approval_dependencies(uuid,uuid,uuid,uuid,bigint,text,text,uuid,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
