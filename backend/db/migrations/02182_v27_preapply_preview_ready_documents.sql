-- +goose Up
-- Private physical readiness predicate. Live editor/source authority,
-- dependency provenance, quota and relational admission remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_documents_ready(p_tenant uuid,p_preview uuid,p_params jsonb,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task uuid;item record;artifact photo.artifacts%ROWTYPE;document photo.backend_documents%ROWTYPE;
 expected_role text;expected_id uuid;expected_sha text;
BEGIN
 IF NOT photo.semantic_preapply_preview_transport_ready(p_preview,p_params,p_node,p_transport,p_inventory) THEN RETURN false;END IF;
 task:=(p_node->>'id')::uuid;
 IF NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_preview
 AND r.kind='preview' AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
 AND r.config_snapshot->'semantic_preapply_preview'->'params'=p_params
 AND r.config_snapshot->'semantic_preapply_preview'->>'params_sha'=p_node->>'params_sha'
 AND r.v27_preapply_preview_profile=p_node->'execution_profile') THEN RETURN false;END IF;
 FOR item IN
 SELECT e->'ref' AS ref,e->>'contract' AS contract FROM jsonb_array_elements(p_inventory->'entries') e
 UNION ALL SELECT p_transport->'chunks'->0,'InputClosureChunk'
 UNION ALL SELECT p_transport->'manifest','InputClosureManifest'
 LOOP
 expected_id:=(item.ref->>'artifact_id')::uuid;expected_sha:=item.ref->>'sha256';
 SELECT * INTO artifact FROM photo.artifacts WHERE tenant_id=p_tenant AND id=expected_id;
 IF artifact.id IS NULL OR artifact.sha256 IS DISTINCT FROM expected_sha OR artifact.state<>'ready'
 OR artifact.verified_at IS NULL OR artifact.object_version IS NULL OR artifact.object_version IN('','null')
 OR artifact.bytes IS NULL OR artifact.bytes NOT BETWEEN 1 AND 9007199254740991
 OR (artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp())
 OR (item.contract='NpyArray' AND coalesce(artifact.media_type,'') NOT IN('application/x-npy','application/octet-stream'))
 OR (item.contract<>'NpyArray' AND artifact.media_type IS DISTINCT FROM 'application/json') THEN RETURN false;END IF;
 IF item.contract IN('Params','InputClosureChunk','InputClosureManifest') THEN
 expected_role:=CASE item.contract WHEN 'Params' THEN 'task_params'
 WHEN 'InputClosureChunk' THEN 'v2_input_chunk_0' ELSE 'v2_input_manifest' END;
 SELECT * INTO document FROM photo.backend_documents d WHERE d.tenant_id=p_tenant
 AND d.run_id=p_preview AND d.owner_id=task AND d.purpose=expected_role;
 IF document.id IS NULL OR document.state<>'ready' OR document.artifact_id IS DISTINCT FROM expected_id
 OR document.sha256 IS DISTINCT FROM expected_sha OR document.bytes IS DISTINCT FROM artifact.bytes
 OR document.bucket IS DISTINCT FROM artifact.bucket OR document.object_version IS DISTINCT FROM artifact.object_version
 OR artifact.output_role IS DISTINCT FROM expected_role
 OR artifact.object_key IS DISTINCT FROM 'tenants/'||p_tenant||'/runs/'||p_preview||'/documents/'||expected_id THEN RETURN false;END IF;
 END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_preapply_preview_documents_ready(uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_documents_ready(uuid,uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
