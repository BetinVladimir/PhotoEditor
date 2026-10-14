-- +goose Up
-- Reuse existing V2 graph/outbox/event transaction after dedicated authority.
-- Registry stays disabled until result adoption is installed.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v29_perspective_lines(p_tenant uuid,p_run uuid,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_witness IS NULL OR pg_column_size(p_witness)>131072 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.cancel_requested
 OR r.v29_perspective_lines_seal_receipt IS NULL
 OR r.v29_perspective_lines_seal_receipt->'witness' IS DISTINCT FROM p_witness THEN RETURN false;END IF;
 -- Acknowledgement only: no task transition or new outbox on exact replay.
 IF r.graph_sealed THEN RETURN r.status IN('queued','running','waiting_review','completed');END IF;
 IF photo.v29_perspective_lines_sealed_source_ready(p_tenant,p_run,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 plan:=r.config_snapshot->'v29_perspective_line_detection';
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(plan->>'task_id')::uuid
 AND operation='optical.perspective_lines' AND operation_version='1' AND node_key='optical.perspective_lines'
 AND resource_class='cpu' AND state='blocked' AND attempt=0 AND NOT cancel_requested)
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run) THEN RETURN false;END IF;
 -- Retains the existing registry/backends/output policy/closure/quota gates,
 -- graph transition, root task readiness, outbox and run-event semantics.
 RETURN photo.seal_v2_graph_pre_v29_lines(p_tenant,p_run);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
