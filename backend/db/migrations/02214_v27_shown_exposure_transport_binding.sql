-- +goose Up
-- Pure bounded transport reconstruction; never grants source or execution authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_exposure_transport_ready(p_child uuid,p_params jsonb,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task uuid;params uuid;entries jsonb;roots jsonb;entry_body text;chunk jsonb;manifest jsonb;
 chunk_sha text;manifest_sha text;required record;n integer;
BEGIN
 IF p_child IS NULL OR p_child='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(p_params) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_node) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'object'
 OR p_params->>'child_run_id' IS DISTINCT FROM p_child::text
 OR jsonb_typeof(p_params->'target_id') IS DISTINCT FROM 'string'
 OR coalesce(p_params->>'target_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR p_params->>'target_id'='00000000-0000-0000-0000-000000000000' THEN RETURN false;END IF;
 IF NOT(p_node ?& ARRAY['id','node_key','operation','operation_version','resource_class','params_artifact_id','params_sha','input_digest','execution_profile'])
 OR (p_node-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'params_artifact_id'-'params_sha'-'input_digest'-'execution_profile')<>'{}'::jsonb
 OR p_node->>'operation' IS DISTINCT FROM 'semantic.adopt_shown_exposure'
 OR p_node->>'operation_version' IS DISTINCT FROM '1' OR p_node->>'resource_class' IS DISTINCT FROM 'cpu'
 OR p_node->>'node_key' IS DISTINCT FROM 'semantic.adopt_shown_exposure:'||(p_params->>'target_id')::uuid::text THEN RETURN false;END IF;
 task:=photo.semantic_uuid_v5(p_child,'semantic.adopt_shown_exposure:'||(p_params->>'target_id')::uuid::text);
 params:=photo.semantic_uuid_v5(task,'Params');
 IF p_node->>'id' IS DISTINCT FROM task::text OR p_node->>'params_artifact_id' IS DISTINCT FROM params::text
 OR p_transport->>'task_id' IS DISTINCT FROM task::text OR p_inventory->>'task_id' IS DISTINCT FROM task::text
 OR NOT(p_inventory ?& ARRAY['task_id','entries']) OR (p_inventory-'task_id'-'entries')<>'{}'::jsonb
 OR NOT(p_transport ?& ARRAY['task_id','entry_count','entries_sha','manifest','chunks','direct_roots'])
 OR (p_transport-'task_id'-'entry_count'-'entries_sha'-'manifest'-'chunks'-'direct_roots')<>'{}'::jsonb THEN RETURN false;END IF;
 entries:=p_inventory->'entries';
 IF photo.semantic_shown_exposure_inventory_digest_ready(task,params,p_node->>'params_sha',
 p_node->>'input_digest',p_transport->>'entries_sha',entries) IS DISTINCT FROM true THEN RETURN false;END IF;
 n:=jsonb_array_length(entries);
 SELECT jsonb_agg(e.value->'ref' ORDER BY e.ordinality),
 '['||string_agg('{"contract":"'||(e.value->>'contract')||'","ref":{"artifact_id":"'||(e.value->'ref'->>'artifact_id')||'","sha256":"'||(e.value->'ref'->>'sha256')||'"}}',',' ORDER BY e.ordinality)||']'
 INTO roots,entry_body FROM jsonb_array_elements(entries) WITH ORDINALITY e(value,ordinality);
 IF p_transport->'direct_roots' IS DISTINCT FROM roots OR p_transport->'entry_count' IS DISTINCT FROM to_jsonb(n)
 OR jsonb_typeof(p_transport->'chunks') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_transport->'chunks')<>1 THEN RETURN false;END IF;
 chunk_sha:=encode(sha256(convert_to('{"entries":'||entry_body||',"schema_version":1}','UTF8')),'hex');
 chunk:=jsonb_build_object('artifact_id',photo.semantic_uuid_v5(task,'v2_input_chunk_0'),'sha256',chunk_sha);
 IF p_transport->'chunks' IS DISTINCT FROM jsonb_build_array(chunk) THEN RETURN false;END IF;
 manifest_sha:=encode(sha256(convert_to('{"chunks":[{"artifact_id":"'||(chunk->>'artifact_id')||'","sha256":"'||chunk_sha||'"}],"entry_count":'||n::text||',"entries_sha":"'||(p_transport->>'entries_sha')||'","schema_version":1,"task_id":"'||task::text||'"}','UTF8')),'hex');
 manifest:=jsonb_build_object('artifact_id',photo.semantic_uuid_v5(task,'v2_input_manifest'),'sha256',manifest_sha);
 IF p_transport->'manifest' IS DISTINCT FROM manifest THEN RETURN false;END IF;
 FOR required IN SELECT * FROM (VALUES
 (p_params->'approved_edit','ApprovedSemanticEdit'),(p_params->'presentation','ExposurePreviewPresentation'),
 (p_params->'numerical_evidence','ExposurePreviewNumericalEvidence'),(p_params->'working_image','WorkingImage'),
 (p_params->'pixels','NpyArray')) AS required_roots(ref,contract) LOOP
 IF required.ref IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(entries) item
 WHERE item->'ref'=required.ref AND item->>'contract'=required.contract) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_exposure_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_exposure_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
