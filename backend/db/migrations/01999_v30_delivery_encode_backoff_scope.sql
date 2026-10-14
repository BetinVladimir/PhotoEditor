-- +goose Up
-- Repeat the pending-encode predicate under tenant, run, then task locks. A
-- candidate page grants no authority after a changed operation profile.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.defer_v30_delivery_encode(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;n integer;run_id uuid;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT x.run_id INTO run_id FROM photo.tasks x WHERE x.tenant_id=p_tenant AND x.id=p_task;
 IF run_id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested
 OR NOT(r.config_snapshot ? 'v30_delivery_export') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF NOT FOUND OR t.run_id IS DISTINCT FROM r.id
 OR t.operation IS DISTINCT FROM 'export.encode'
 OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR t.state<>'succeeded' OR t.cancel_requested
 OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
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
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
