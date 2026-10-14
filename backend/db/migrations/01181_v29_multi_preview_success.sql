-- +goose Up
-- Scoped completion repeats both immutable encoded receipts under lifecycle locks.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_successful_v29_multi_preview(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;d photo.backend_documents%ROWTYPE;variant text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v29_multi_preview_task_id IS NULL OR r.kind<>'preview'
  OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run ORDER BY id FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>2
  OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
   AND (state<>'succeeded' OR cancel_requested OR operation<>'media.preview' OR operation_version<>'1'))
  OR (SELECT count(*) FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND purpose='task_params')<>2 THEN RETURN false;END IF;
 FOREACH variant IN ARRAY ARRAY['preview1600','preview2560'] LOOP
  SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.tasks t ON t.tenant_id=b.tenant_id AND t.id=b.owner_id
  WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.purpose='task_params'
   AND t.node_key='render.derivative:'||variant FOR UPDATE OF b;
  IF NOT FOUND OR d.state<>'ready' OR d.v29_multi_preview_receipt IS NULL THEN RETURN false;END IF;
  IF NOT photo.record_v29_multi_preview_receipt(p_tenant,p_run,variant,
    (d.v29_multi_preview_receipt->>'result_id')::uuid,
    (d.v29_multi_preview_receipt->>'attempt')::integer,
    (d.v29_multi_preview_receipt->>'fence')::bigint,
    d.v29_multi_preview_receipt->>'manifest',d.v29_multi_preview_receipt->'locations') THEN RETURN false;END IF;
 END LOOP;
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_successful_v29_multi_preview(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_successful_v29_multi_preview(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.finalize_successful_v29_multi_preview(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
