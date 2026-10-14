-- +goose Up
-- Retained render qualification/profile for one exact delegated ancestor task.
-- Caller decodes against physically read FrozenRenderRecipe and rechecks root.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_qualification(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;result jsonb;
BEGIN
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'render.compose' OR e->>'version'<>'1'
  THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('pin',r.v26_qualification_pin,
  'profile',t.execution_profile)
 INTO result FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 WHERE t.tenant_id=(source->>'tenant_id')::uuid AND t.id=p_task
  AND t.run_id=(e->>'run')::uuid AND t.canonical_result_id=(e->>'result')::uuid
  AND t.attempt=(e->>'attempt')::integer
  AND t.fencing_token=(e->>'fence')::bigint
  AND t.state='succeeded' AND NOT t.cancel_requested
  AND t.operation='render.compose' AND t.operation_version='1'
  AND w.project_id=(source->>'project_id')::uuid
  AND w.workflow_type='preview' AND w.renderer_version='v2.2'
  AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
  AND r.v26_qualification_pin IS NOT NULL
 FOR SHARE OF t,r;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_qualification(uuid,uuid,integer,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_qualification(uuid,uuid,integer,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_qualification(uuid,uuid,integer,uuid,uuid)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
