-- +goose Up
-- Private transport readiness gate. document_run is the server-selected durable
-- publication scope (parent before child creation, or child after creation).
-- Caller must prove this scope in the atomic Apply transaction; this function
-- alone grants neither cross-run reads nor permission to activate tasks.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_exposure_transport_documents_ready(p_tenant uuid,p_document_run uuid,p_child uuid,
 p_params jsonb,p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task uuid;item record;aid uuid;digest text;a photo.artifacts%ROWTYPE;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant='00000000-0000-0000-0000-000000000000'::uuid
 OR p_document_run IS NULL OR p_document_run='00000000-0000-0000-0000-000000000000'::uuid
 OR photo.semantic_shown_exposure_transport_ready(p_child,p_params,p_node,p_transport,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_shown_exposure_input_objects_ready(p_tenant,p_inventory->'entries') IS DISTINCT FROM true THEN RETURN false;END IF;
 task:=(p_node->>'id')::uuid;
 FOR item IN SELECT * FROM (VALUES
 (jsonb_build_object('artifact_id',p_node->>'params_artifact_id','sha256',p_node->>'params_sha'),'task_params'),
 (p_transport->'chunks'->0,'v2_input_chunk_0'),(p_transport->'manifest','v2_input_manifest')) AS docs(ref,purpose)
 LOOP
  aid:=(item.ref->>'artifact_id')::uuid;digest:=item.ref->>'sha256';
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=aid;
  IF NOT FOUND OR a.sha256 IS DISTINCT FROM digest OR a.state IS DISTINCT FROM 'ready'
  OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
  OR a.bytes IS NULL OR a.bytes NOT BETWEEN 1 AND 1048576
  OR a.media_type IS DISTINCT FROM 'application/json' OR coalesce(a.bucket,'')=''
  OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())
  OR a.output_role IS DISTINCT FROM item.purpose
  OR a.object_key IS DISTINCT FROM 'tenants/'||p_tenant||'/runs/'||p_document_run||'/documents/'||aid THEN RETURN false;END IF;
  SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_document_run
  AND owner_id=task AND purpose=item.purpose;
  IF NOT FOUND OR d.state IS DISTINCT FROM 'ready' OR d.artifact_id IS DISTINCT FROM aid
  OR d.sha256 IS DISTINCT FROM digest OR d.bytes IS DISTINCT FROM a.bytes
  OR d.bucket IS DISTINCT FROM a.bucket OR d.object_version IS DISTINCT FROM a.object_version THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_exposure_transport_documents_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_exposure_transport_documents_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
