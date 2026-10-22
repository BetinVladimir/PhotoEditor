-- +goose Up
-- Local signed ancestry is transported as a separate frozen authorization root.
-- Original archive producer proof remains independent and cannot become local.
-- +goose StatementBegin
ALTER FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) RENAME TO v30_interchange_prepared_scope_without_historical_origin;
CREATE FUNCTION photo.v30_interchange_prepared_scope(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt jsonb;occ jsonb;parent photo.backend_documents%ROWTYPE;checked jsonb;sources jsonb;boxes uuid;boxes_sha text;ref jsonb;field text;
BEGIN
 IF p_contract IS DISTINCT FROM 'HistoricalOriginReceipt' THEN RETURN photo.v30_interchange_prepared_scope_without_historical_origin(p_tenant,p_workflow,p_parsed,p_artifact,p_contract,p_body);END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 receipt:=p_body::jsonb;occ:=receipt->'historical_origin_occurrence';
 IF jsonb_typeof(receipt) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(receipt))<>6 OR NOT(receipt ?& ARRAY['entry_id','binding','sha256','bytes','media_type','historical_origin_occurrence'])
 OR jsonb_typeof(occ) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(occ))<>16
 OR NOT(occ ?& ARRAY['schema','tenant_id','project_id','workflow_id','run_id','entry_id','target','original_source','contract','original','prepared','original_image','original_boxes','model_lock_id','backend','producer_mode'])
 OR occ->>'schema' IS DISTINCT FROM 'historical-origin-payload/v1' OR occ->>'producer_mode' IS DISTINCT FROM 'local_canonical'
 OR occ->>'tenant_id' IS DISTINCT FROM p_tenant::text OR occ->>'workflow_id' IS DISTINCT FROM p_workflow::text
 OR occ->>'entry_id' IS DISTINCT FROM receipt->>'entry_id' OR occ->'target' IS DISTINCT FROM receipt->'binding'
 OR coalesce(occ->>'contract','') NOT IN('WorkingImage','FaceBoxes','FaceLandmarks')
 OR receipt->>'media_type' IS DISTINCT FROM 'application/json' OR jsonb_typeof(receipt->'bytes') IS DISTINCT FROM 'number' OR coalesce(receipt->>'bytes','') !~ '^[1-9][0-9]*$'
 OR (receipt->>'bytes')::bigint NOT BETWEEN 2 AND 65536 OR coalesce(receipt->>'sha256','') !~ '^[0-9a-f]{64}$'
 OR p_artifact IS DISTINCT FROM photo.v30_uuid5(p_workflow,'interchange:historical-origin:'||(receipt->>'entry_id')||':'||(occ->>'contract')||':'||(occ#>>'{original,artifact_id}')) THEN RETURN NULL;END IF;
 FOREACH field IN ARRAY ARRAY['original_source','original','prepared','original_image'] LOOP
  ref:=occ->field;
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(ref))<>2 OR NOT(ref ?& ARRAY['artifact_id','sha256'])
  OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' OR (ref->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN NULL;END IF;
 END LOOP;
 IF occ->>'contract'='WorkingImage' THEN
  IF occ->'original_image' IS DISTINCT FROM occ->'original' OR occ->'original_boxes' IS DISTINCT FROM 'null'::jsonb OR occ->>'model_lock_id' IS DISTINCT FROM '' OR occ->>'backend' IS DISTINCT FROM '' THEN RETURN NULL;END IF;
 ELSE
  IF coalesce(occ->>'model_lock_id','') !~ '^[0-9a-f]{64}$' OR coalesce(occ->>'backend','') NOT IN('cpu','mps','cuda') THEN RETURN NULL;END IF;
  IF occ->>'contract'='FaceBoxes' AND occ->'original_boxes' IS DISTINCT FROM 'null'::jsonb THEN RETURN NULL;END IF;
  IF occ->>'contract'='FaceLandmarks' THEN
   ref:=occ->'original_boxes';
   IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(ref))<>2 OR NOT(ref ?& ARRAY['artifact_id','sha256'])
   OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
   OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' OR (ref->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN NULL;END IF;
  END IF;
 END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM occ->>'project_id' THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.backend_documents WHERE tenant_id=p_tenant AND artifact_id=(occ#>>'{prepared,artifact_id}')::uuid AND sha256=occ#>>'{prepared,sha256}' AND state='ready' AND purpose='v30_import_prepared' FOR SHARE;
 IF NOT FOUND OR parent.run_id::text IS DISTINCT FROM occ->>'run_id' OR parent.v30_import_prepared->'origin' IS NULL
 OR parent.v30_import_prepared#>'{origin,artifact_id}' IS DISTINCT FROM occ#>'{original,artifact_id}' OR parent.v30_import_prepared#>'{origin,sha256}' IS DISTINCT FROM occ#>'{original,sha256}' THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v30_interchange_prepared_document(p_tenant,p_workflow,p_parsed,(receipt->>'entry_id')::uuid,receipt->'binding',parent.artifact_id,parent.sha256,occ->>'contract');IF NOT FOUND THEN RETURN NULL;END IF;
 IF occ->'original_boxes' IS DISTINCT FROM 'null'::jsonb THEN boxes:=(occ#>>'{original_boxes,artifact_id}')::uuid;boxes_sha:=occ#>>'{original_boxes,sha256}';END IF;
 IF photo.v30_import_historical_document_authorized(p_tenant,p_workflow,p_parsed,(occ#>>'{original_source,artifact_id}')::uuid,occ#>>'{original_source,sha256}',occ->>'contract',(occ#>>'{original,artifact_id}')::uuid,occ#>>'{original,sha256}',(occ#>>'{original_image,artifact_id}')::uuid,occ#>>'{original_image,sha256}',occ->>'model_lock_id',occ->>'backend',boxes,boxes_sha) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN parent.v30_import_prepared||jsonb_build_object('owner',p_artifact,'contract',p_contract,'sha',receipt->>'sha256','bytes',(receipt->>'bytes')::bigint,'media_type','application/json','historical_origin_occurrence',occ);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text),photo.v30_interchange_prepared_scope_without_historical_origin(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
DO $$ DECLARE definition text;anchor text:=' certificate:=photo.v30_interchange_prepared_scope(';BEGIN
 definition:=pg_get_functiondef('photo.v30_interchange_prepared_document(uuid,uuid,text,uuid,jsonb,uuid,text,text)'::regprocedure);
 IF definition IS NULL OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'HISTORICAL_RECEIPT_DOCUMENT_SHAPE';END IF;
 definition:=replace(definition,anchor,' IF d.v30_import_prepared ? ''historical_origin_occurrence'' THEN receipt:=receipt||jsonb_build_object(''historical_origin_occurrence'',d.v30_import_prepared->''historical_origin_occurrence'');END IF;'||E'\n'||anchor);EXECUTE definition;
END $$;
-- Repeat generated receipt authority during every adoption input-inventory
-- recheck. A valid signature does not preserve a revoked native policy/ACL.
DO $$ DECLARE definition text;anchor text:=' RETURN QUERY SELECT i.ordinal,i.artifact_id,i.sha256,i.contract,a.media_type';BEGIN
 definition:=pg_get_functiondef('photo.v30_import_adoption_input_inventory(uuid,uuid,integer,bigint,uuid)'::regprocedure);
 IF definition IS NULL OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'HISTORICAL_ADOPTION_INVENTORY_SHAPE';END IF;
 definition:=replace(definition,anchor,' IF EXISTS(SELECT FROM photo.v2_task_inputs hi WHERE hi.tenant_id=p_tenant AND hi.task_id=p_task AND hi.contract=''HistoricalOriginReceipt'' AND NOT EXISTS(SELECT FROM photo.backend_documents hd JOIN photo.v2_workflows hw ON hw.tenant_id=hd.tenant_id AND hw.run_id=hd.run_id JOIN photo.backend_documents he ON he.tenant_id=hd.tenant_id AND he.run_id=hd.run_id AND he.owner_id=p_task AND he.purpose=''v30_interchange_import_evidence'' AND he.state=''ready'' WHERE hd.tenant_id=hi.tenant_id AND hd.artifact_id=hi.artifact_id AND hd.sha256=hi.sha256 AND hd.state=''ready'' AND hd.purpose=''v30_import_prepared'' AND EXISTS(SELECT FROM photo.v30_interchange_prepared_document(p_tenant,hw.id,he.v30_import_parsed_jcs,(hd.v30_import_prepared->>''entry'')::uuid,hd.v30_import_prepared->''binding'',hd.artifact_id,hd.sha256,''HistoricalOriginReceipt'')))) THEN RETURN;END IF;'||E'\n'||anchor);
 EXECUTE definition;
END $$;
-- Worker admission repeats the same current receipt gate before computation.
DO $$ DECLARE definition text;anchor text:=' RETURN jsonb_build_object(''workflow_id'',w.id';BEGIN
 definition:=pg_get_functiondef('photo.v30_interchange_import_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'::regprocedure);
 IF definition IS NULL OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'HISTORICAL_WORKER_ADMISSION_SHAPE';END IF;
 definition:=replace(definition,anchor,' IF EXISTS(SELECT FROM photo.v2_task_inputs hi WHERE hi.tenant_id=p_tenant AND hi.task_id=p_task AND hi.contract=''HistoricalOriginReceipt'' AND NOT EXISTS(SELECT FROM photo.backend_documents hd WHERE hd.tenant_id=hi.tenant_id AND hd.run_id=p_run AND hd.artifact_id=hi.artifact_id AND hd.sha256=hi.sha256 AND hd.state=''ready'' AND hd.purpose=''v30_import_prepared'' AND EXISTS(SELECT FROM photo.v30_interchange_prepared_document(p_tenant,w.id,e.v30_import_parsed_jcs,(hd.v30_import_prepared->>''entry'')::uuid,hd.v30_import_prepared->''binding'',hd.artifact_id,hd.sha256,''HistoricalOriginReceipt'')))) THEN RETURN NULL;END IF;'||E'\n'||anchor);EXECUTE definition;
END $$;
CREATE FUNCTION photo.v30_import_historical_adoption_run(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ DECLARE rid uuid;BEGIN
 IF photo.v30_interchange_import_adoption_context(p_tenant,p_task,p_attempt,p_fence,p_result) IS NULL THEN RETURN NULL;END IF;
 SELECT run_id INTO rid FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;RETURN rid;
END $$;
ALTER FUNCTION photo.v30_import_historical_adoption_run(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_historical_adoption_run(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_historical_adoption_run(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
