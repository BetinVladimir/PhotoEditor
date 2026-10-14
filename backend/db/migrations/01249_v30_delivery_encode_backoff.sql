-- +goose Up
-- Scheduling hints only; never change canonical producer state or reject outputs.
ALTER TABLE photo.tasks ADD COLUMN v30_encode_retry_result uuid;
ALTER TABLE photo.tasks ADD COLUMN v30_encode_retry_count integer NOT NULL DEFAULT 0 CHECK(v30_encode_retry_count BETWEEN 0 AND 7);
ALTER TABLE photo.tasks ADD COLUMN v30_encode_retry_at timestamptz;
-- +goose StatementBegin
CREATE FUNCTION photo.defer_v30_delivery_encode(p_tenant uuid,p_task uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;n integer;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF NOT FOUND OR t.operation<>'export.encode' OR t.state<>'succeeded' OR t.cancel_requested
 OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested
 OR NOT(r.config_snapshot ? 'v30_delivery_export')
 OR EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)) THEN RETURN false;END IF;
 IF t.v30_encode_retry_result=t.canonical_result_id AND t.v30_encode_retry_at>clock_timestamp() THEN RETURN true;END IF;
 n:=CASE WHEN t.v30_encode_retry_result=t.canonical_result_id THEN least(7,t.v30_encode_retry_count+1) ELSE 1 END;
 UPDATE photo.tasks SET v30_encode_retry_result=t.canonical_result_id,v30_encode_retry_count=n,
 v30_encode_retry_at=clock_timestamp()+make_interval(secs=>least(300,5*(2^(n-1))::integer))
 WHERE tenant_id=p_tenant AND id=p_task;
 RETURN true;
END $$;
ALTER FUNCTION photo.defer_v30_delivery_encode(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.defer_v30_delivery_encode(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.defer_v30_delivery_encode(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_delivery_pending_encodes(p_limit integer)
RETURNS TABLE(tenant_id uuid,task_id uuid)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.id FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id
 WHERE p_limit BETWEEN 1 AND 64 AND t.operation='export.encode' AND t.operation_version='1'
 AND t.resource_class='cpu' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.canonical_result_id IS NOT NULL AND t.execution_profile->>'renderer_version'='v2.2'
 AND (t.v30_encode_retry_result IS DISTINCT FROM t.canonical_result_id OR t.v30_encode_retry_at IS NULL OR t.v30_encode_retry_at<=clock_timestamp())
 AND r.kind='v2' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND r.config_snapshot ? 'v30_delivery_export' AND tenant.status='active'
 AND NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 ORDER BY t.created_at,t.id LIMIT CASE WHEN p_limit BETWEEN 1 AND 64 THEN p_limit ELSE 0 END;
$$;
ALTER FUNCTION photo.v30_delivery_pending_encodes(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_pending_encodes(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_pending_encodes(integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
