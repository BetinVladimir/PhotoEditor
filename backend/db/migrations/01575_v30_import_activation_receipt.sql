-- +goose Up
-- Historical admission evidence lives with its tenant-scoped task and is
-- deleted by the existing task lifecycle. It is never execution authority.
-- +goose StatementBegin
ALTER TABLE photo.tasks ADD COLUMN v30_import_activation jsonb;
ALTER TABLE photo.tasks ADD CONSTRAINT tasks_v30_import_activation_shape CHECK
 (v30_import_activation IS NULL OR (operation='v2.interchange.import' AND jsonb_typeof(v30_import_activation)='object'));
CREATE FUNCTION photo.read_v30_interchange_import_activation(p_tenant uuid,p_workflow uuid,p_parsed text,p_plan text,p_params text,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;t photo.tasks%ROWTYPE;e photo.backend_documents%ROWTYPE;expected jsonb;sources jsonb;metadata jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR p_plan IS NULL OR octet_length(p_plan) NOT BETWEEN 2 AND 8388608
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
 OR jsonb_typeof(p_task) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange' AND renderer_version='v2.2';
 IF w.id IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id AND kind='v2' AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=w.run_id AND id=photo.v30_uuid5(w.id,'v2.interchange.import:0') FOR SHARE;
 expected:=jsonb_build_object('schema_version',1,'workflow_id',w.id,'run_id',w.run_id,'task',p_task,
 'parsed_sha',encode(sha256(convert_to(p_parsed,'UTF8')),'hex'),'plan_sha',encode(sha256(convert_to(p_plan,'UTF8')),'hex'),
 'params_sha',encode(sha256(convert_to(p_params,'UTF8')),'hex'));
 IF t.id IS NULL OR t.v30_import_activation IS DISTINCT FROM expected THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=w.run_id AND owner_id=t.id
 AND purpose='v30_interchange_import_evidence' AND artifact_id=photo.v30_uuid5(t.id,'interchange:evidence:1') AND state='ready' FOR SHARE;
 IF e.id IS NULL OR e.v30_import_evidence IS NULL THEN RETURN false;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,w.id,p_parsed);
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,w.id,p_parsed);
 IF sources IS NULL OR metadata IS NULL OR e.v30_import_evidence->'sources' IS DISTINCT FROM sources
 OR e.v30_import_evidence->'metadata' IS DISTINCT FROM metadata THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.read_v30_interchange_import_activation(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_interchange_import_activation(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_interchange_import_activation(uuid,uuid,text,text,text,jsonb) TO photo_scheduler;
ALTER FUNCTION photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb) RENAME TO activate_v30_interchange_import_pre_receipt;
REVOKE ALL ON FUNCTION photo.activate_v30_interchange_import_pre_receipt(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.activate_v30_interchange_import(p_tenant uuid,p_workflow uuid,p_parsed text,p_plan text,p_params text,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;receipt jsonb;
BEGIN
 IF photo.read_v30_interchange_import_activation(p_tenant,p_workflow,p_parsed,p_plan,p_params,p_task) THEN RETURN true;END IF;
 IF photo.activate_v30_interchange_import_pre_receipt(p_tenant,p_workflow,p_parsed,p_plan,p_params,p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 receipt:=jsonb_build_object('schema_version',1,'workflow_id',w.id,'run_id',w.run_id,'task',p_task,
 'parsed_sha',encode(sha256(convert_to(p_parsed,'UTF8')),'hex'),'plan_sha',encode(sha256(convert_to(p_plan,'UTF8')),'hex'),
 'params_sha',encode(sha256(convert_to(p_params,'UTF8')),'hex'));
 UPDATE photo.tasks SET v30_import_activation=receipt WHERE tenant_id=p_tenant AND run_id=w.run_id
 AND id=photo.v30_uuid5(w.id,'v2.interchange.import:0') AND state='ready' AND v30_import_activation IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_ACTIVATION_RECEIPT_CONFLICT' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
