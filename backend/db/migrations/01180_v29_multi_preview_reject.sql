-- +goose Up
-- Reject only the exact canonical result whose immutable bytes failed validation.
-- +goose StatementBegin
CREATE FUNCTION photo.reject_v29_multi_preview(p_tenant uuid,p_run uuid,p_variant text,p_result uuid,p_attempt integer,p_fence bigint,p_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 IF p_variant IS NULL OR p_variant NOT IN('preview1600','preview2560') THEN RETURN false;END IF;
 IF p_code IS NULL OR p_code NOT IN('RENDER_DERIVATIVE_ENCODING_INVALID','RENDER_DERIVATIVE_CONTRACT_INVALID') THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v29_multi_preview_task_id IS NULL OR r.kind<>'preview' OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND node_key='render.derivative:'||p_variant FOR UPDATE;
 IF NOT FOUND OR t.operation<>'media.preview' OR t.operation_version<>'1' OR t.cancel_requested
 OR t.canonical_result_id IS DISTINCT FROM p_result OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence THEN RETURN false;END IF;
 IF t.state='failed' THEN RETURN t.terminal_reason=p_code;END IF;
 IF t.state<>'succeeded' OR EXISTS(SELECT 1 FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=t.id AND v29_multi_preview_receipt IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM photo.v29_multi_preview_context(p_tenant,p_run,p_variant) c
  WHERE c.task_id=t.id AND c.result_id=p_result AND c.attempt=p_attempt AND c.fence=p_fence) THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='failed',terminal_reason=p_code WHERE tenant_id=p_tenant AND id=t.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reject_v29_multi_preview(uuid,uuid,text,uuid,integer,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reject_v29_multi_preview(uuid,uuid,text,uuid,integer,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reject_v29_multi_preview(uuid,uuid,text,uuid,integer,bigint,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
