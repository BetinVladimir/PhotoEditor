-- +goose Up
-- Acquire the complete batch qualification lock set before per-node checks.
-- Task UUID order alone does not order policy/report identities across batches.
-- +goose StatementBegin
ALTER FUNCTION photo.semantic_approved_exposure_nodes_ready(uuid,uuid,uuid,jsonb,jsonb) RENAME TO semantic_approved_exposure_nodes_pre_batch_locks;
REVOKE ALL ON FUNCTION photo.semantic_approved_exposure_nodes_pre_batch_locks(uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_api,photo_scheduler,photo_worker;
CREATE FUNCTION photo.semantic_approved_exposure_nodes_ready(p_tenant uuid,p_project uuid,p_child uuid,p_params jsonb,p_nodes jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF jsonb_typeof(p_nodes) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_nodes) NOT BETWEEN 1 AND 100 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation='semantic.local_exposure' AND version='1' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.v2_qualification_policies
 WHERE id IN(SELECT (n->'execution_profile'->'qualification'->>'policy_id')::uuid FROM jsonb_array_elements(p_nodes) n)
 ORDER BY id FOR SHARE;
 PERFORM id FROM photo.model_qualifications WHERE tenant_id=p_tenant AND project_id=p_project
 AND id IN(SELECT (n->'execution_profile'->'qualification'->>'qualification_id')::uuid FROM jsonb_array_elements(p_nodes) n)
 ORDER BY id FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(
 SELECT (n->'execution_profile'->'qualification'->r->>'artifact_id')::uuid
 FROM jsonb_array_elements(p_nodes) n CROSS JOIN unnest(ARRAY['report','calibration']) r)
 ORDER BY id FOR SHARE;
 RETURN photo.semantic_approved_exposure_nodes_pre_batch_locks(p_tenant,p_project,p_child,p_params,p_nodes);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_approved_exposure_nodes_ready(uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_exposure_nodes_ready(uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
