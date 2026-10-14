-- +goose Up
-- Both encoded-verification receipts must remain current before completion.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_successful_v26_render_derivative()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;d photo.backend_documents%ROWTYPE;variant text;
BEGIN
 SELECT x.tenant_id,x.id INTO candidate FROM photo.runs x
 WHERE x.v26_derivative_task_id IS NOT NULL AND x.kind='preview' AND x.status='running' AND x.graph_sealed AND NOT x.cancel_requested
 AND (SELECT count(*) FROM photo.backend_documents b WHERE b.tenant_id=x.tenant_id AND b.run_id=x.id AND b.purpose='task_params' AND b.v26_derivative_receipt IS NOT NULL)=2
 AND (SELECT count(*) FROM photo.v26_render_derivative_result(x.tenant_id,x.id,'preview1600'))=2
 AND (SELECT count(*) FROM photo.v26_render_derivative_result(x.tenant_id,x.id,'preview2560'))=2
 ORDER BY x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=candidate.tenant_id FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT FOUND OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id)<>2
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id AND (state<>'succeeded' OR cancel_requested)) THEN RETURN false;END IF;
 FOREACH variant IN ARRAY ARRAY['preview1600','preview2560'] LOOP
  SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.tasks t ON t.tenant_id=b.tenant_id AND t.id=b.owner_id
  WHERE b.tenant_id=r.tenant_id AND b.run_id=r.id AND b.purpose='task_params'
  AND t.node_key='render.derivative:'||variant FOR UPDATE OF b;
  IF NOT FOUND OR d.state<>'ready' OR d.v26_derivative_receipt IS NULL THEN RETURN false;END IF;
  IF NOT photo.record_v26_render_derivative_receipt(r.tenant_id,r.id,variant,
   (d.v26_derivative_receipt->>'result_id')::uuid,(d.v26_derivative_receipt->>'attempt')::integer,
   (d.v26_derivative_receipt->>'fence')::bigint,d.v26_derivative_receipt->>'manifest') THEN RETURN false;END IF;
 END LOOP;
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_successful_v26_render_derivative() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_successful_v26_render_derivative() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_successful_v26_render_derivative() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
