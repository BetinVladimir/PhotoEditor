-- +goose Up
-- Read acknowledgement requires current source and committed parent cursor.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_initial_child_activated(p_expansion uuid,p_token uuid,p_slot integer,p_request text,
 p_export uuid,p_task jsonb,p_params text,p_image text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;scope jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;operation text;
BEGIN
 IF p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 2097152 OR p_task IS NULL OR p_export IS NULL THEN RETURN false;END IF;
 operation:=p_task->>'operation';
 IF operation='derivative.watermark' THEN
  scope:=photo.v30_watermark_initial_params_scope(p_expansion,p_token,p_slot,p_params,p_image);
 ELSIF operation IN('derivative.resize','derivative.sharpen') THEN
  scope:=photo.v30_watermark_derivative_initial_params_scope(p_expansion,p_token,p_slot,p_params);
 ELSE RETURN false;END IF;
 IF scope IS NULL THEN RETURN false;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL OR expected IS DISTINCT FROM p_request::jsonb THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 IF p_slot>=e.cursor::integer THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM expected
 OR r.config_snapshot->>'export_id' IS DISTINCT FROM p_export::text THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=(scope->>'owner')::uuid FOR SHARE;
 IF NOT FOUND OR t.state NOT IN('ready','leased','running','succeeded','failed','cancelled','skipped')
 OR t.id::text IS DISTINCT FROM p_task->>'id' OR t.node_key IS DISTINCT FROM p_task->>'node_key'
 OR t.operation IS DISTINCT FROM operation OR t.operation_version IS DISTINCT FROM p_task->>'operation_version'
 OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
 OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=e.tenant_id AND d.run_id=e.run_id
 AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 AND d.sha256=scope->>'sha' AND d.sha256=p_task->>'params_sha' AND d.bytes=(scope->>'bytes')::bigint) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.delivery_items i JOIN photo.delivery_packages d ON d.tenant_id=i.tenant_id AND d.id=i.package_id
 JOIN photo.v2_workflows w ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id AND w.run_id=e.run_id
 WHERE i.tenant_id=e.tenant_id AND i.ordinal=(expected->>'ordinal')::integer
 AND i.preset_id=(expected->'preset'->>'preset_id')::uuid AND i.preset_sha=expected->'preset'->>'preset_sha'
 AND i.export_id=p_export) THEN RETURN false;END IF;
 RETURN e.expansion_lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_watermark_initial_child_activated(uuid,uuid,integer,text,uuid,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_initial_child_activated(uuid,uuid,integer,text,uuid,jsonb,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_watermark_initial_child_activated(uuid,uuid,integer,text,uuid,jsonb,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
