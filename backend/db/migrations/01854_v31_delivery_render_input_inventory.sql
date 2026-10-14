-- +goose Up
-- Registered, pre-transport render input inventory under the exact active
-- Delivery signer delegation. The caller must still traverse every occurrence,
-- verify physical bytes and compare the canonical task input digest.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_input_inventory(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;result jsonb;
BEGIN
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'render.compose' OR e->>'version'<>'1'
  THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object(
   'artifact_id',i.artifact_id,'sha256',i.sha256),
   'contract',i.contract) ORDER BY i.artifact_id)
 INTO result FROM photo.v2_task_inputs i
 JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id
  AND w.project_id=i.project_id AND w.run_id=(e->>'run')::uuid
 WHERE i.tenant_id=(source->>'tenant_id')::uuid AND i.task_id=p_task
  AND i.project_id=(source->>'project_id')::uuid
  AND i.ordinal<1000000 AND i.contract NOT IN
   ('InputClosureChunk','InputClosureManifest')
  AND w.workflow_type='preview' AND w.renderer_version='v2.2';
 IF result IS NULL OR jsonb_array_length(result)<2 OR
  jsonb_array_length(result)>4096 THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i
  WHERE i.tenant_id=(source->>'tenant_id')::uuid AND i.task_id=p_task
   AND i.project_id=(source->>'project_id')::uuid AND i.ordinal<1000000
   AND i.contract IN('InputClosureChunk','InputClosureManifest'))
  THEN RETURN NULL;END IF;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_input_inventory(uuid,uuid,integer,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_input_inventory(uuid,uuid,integer,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_input_inventory(uuid,uuid,integer,uuid,uuid)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
