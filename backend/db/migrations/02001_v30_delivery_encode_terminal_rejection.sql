-- +goose Up
-- A malformed immutable encode result must not retry forever. Only seven
-- confirmed semantic refusals of the same current result permit terminal
-- V2 rejection. IO failures never call the backoff/terminal path.
-- +goose StatementBegin
CREATE FUNCTION photo.reject_v30_delivery_encode_after_retries(
 p_tenant uuid,p_task uuid,p_code text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;run_id uuid;
BEGIN
 IF p_code IS NULL OR p_code !~ '^[A-Z][A-Z0-9_]{0,79}$' THEN
  RAISE EXCEPTION 'DELIVERY_ENCODE_REJECTION_CODE_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT x.run_id INTO run_id FROM photo.tasks x WHERE x.tenant_id=p_tenant AND x.id=p_task;
 IF run_id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested
 OR NOT r.graph_sealed OR NOT (r.config_snapshot ? 'v30_delivery_export') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF NOT FOUND OR t.run_id IS DISTINCT FROM r.id
 OR t.operation IS DISTINCT FROM 'export.encode' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.canonical_result_id IS NULL
 OR t.v30_encode_retry_result IS DISTINCT FROM t.canonical_result_id
 OR t.v30_encode_retry_count<>7 OR t.v30_encode_retry_at IS NULL THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v30_delivery_encode_context(p_tenant,p_task)) THEN RETURN false;END IF;
 RETURN photo.reject_v2_adoption(p_tenant,p_task,t.canonical_result_id,p_code);
END $$;
ALTER FUNCTION photo.reject_v30_delivery_encode_after_retries(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reject_v30_delivery_encode_after_retries(uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reject_v30_delivery_encode_after_retries(uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
