-- +goose Up
-- Metadata results are immutable. Only physical/result-contract refusals of the
-- exact current attempt/fence count; authority and IO failures remain retries.
ALTER TABLE photo.tasks ADD COLUMN v30_metadata_retry_result uuid;
ALTER TABLE photo.tasks ADD COLUMN v30_metadata_retry_count integer NOT NULL DEFAULT 0
 CHECK (v30_metadata_retry_count BETWEEN 0 AND 7);
ALTER TABLE photo.tasks ADD COLUMN v30_metadata_retry_at timestamptz;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.next_v30_metadata_adoption(p_after uuid)
RETURNS TABLE(tenant_id uuid,task_id uuid,attempt integer,fence bigint,result_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;previous text:=current_setting('app.tenant_id',true);
BEGIN
 IF p_after IS NULL THEN RETURN;END IF;
 FOR candidate IN SELECT t.tenant_id,t.id,t.attempt,t.fencing_token,t.canonical_result_id FROM photo.tasks t
 WHERE t.id>p_after AND t.operation='metadata.write' AND t.operation_version='1' AND t.state='succeeded'
 AND NOT t.cancel_requested AND t.canonical_result_id IS NOT NULL
 AND (t.v30_metadata_retry_result IS DISTINCT FROM t.canonical_result_id
  OR t.v30_metadata_retry_at IS NULL OR t.v30_metadata_retry_at<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=t.tenant_id AND consumer='v2-adopt'
  AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 ORDER BY t.id LOOP
  PERFORM set_config('app.tenant_id',candidate.tenant_id::text,true);
  IF photo.v30_metadata_result_ready(candidate.tenant_id,candidate.id,candidate.attempt,
   candidate.fencing_token,candidate.canonical_result_id) IS TRUE THEN
   PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
   RETURN QUERY SELECT candidate.tenant_id,candidate.id,candidate.attempt,candidate.fencing_token,candidate.canonical_result_id;RETURN;
  END IF;
 END LOOP;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
END $$;
ALTER FUNCTION photo.next_v30_metadata_adoption(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v30_metadata_adoption(uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.next_v30_metadata_adoption(uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.settle_v30_metadata_invalid(p_tenant uuid,p_task uuid,p_attempt integer,
 p_fence bigint,p_result uuid,p_code text) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;run_id uuid;n integer;
BEGIN
 IF p_code NOT IN ('DELIVERY_METADATA_DOCUMENT_EVIDENCE','DELIVERY_METADATA_DOCUMENT_BYTES',
  'DELIVERY_METADATA_DOCUMENT_CANONICAL','DELIVERY_METADATA_RASTER_EVIDENCE',
  'DELIVERY_METADATA_RASTER_STREAM','DELIVERY_METADATA_RASTER_BYTES',
  'DELIVERY_METADATA_RESULT_ALIAS','DELIVERY_METADATA_PROJECTION_BYTES',
  'DELIVERY_METADATA_OUTPUT_CLOSURE','DELIVERY_METADATA_PHYSICAL_INVALID') THEN
  RAISE EXCEPTION 'METADATA_ADOPTION_CODE_INVALID' USING ERRCODE='22023';
 END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN 'stale';END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN 'stale';END IF;
 SELECT x.run_id INTO run_id FROM photo.tasks x WHERE x.tenant_id=p_tenant AND x.id=p_task;
 IF run_id IS NULL THEN RETURN 'stale';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested
 OR NOT r.graph_sealed OR NOT(r.config_snapshot ? 'v30_delivery_export') THEN RETURN 'stale';END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF NOT FOUND OR t.run_id IS DISTINCT FROM r.id OR t.operation IS DISTINCT FROM 'metadata.write'
 OR t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.canonical_result_id IS DISTINCT FROM p_result
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
  AND event_id=photo.v2_adoption_event_id(p_task,p_result))
 OR photo.v30_metadata_result_ready(p_tenant,p_task,p_attempt,p_fence,p_result) IS DISTINCT FROM true
 THEN RETURN 'stale';END IF;
 IF t.v30_metadata_retry_result=p_result AND t.v30_metadata_retry_at>clock_timestamp() THEN RETURN 'deferred';END IF;
 n:=CASE WHEN t.v30_metadata_retry_result=p_result THEN least(7,t.v30_metadata_retry_count+1) ELSE 1 END;
 UPDATE photo.tasks SET v30_metadata_retry_result=p_result,v30_metadata_retry_count=n,
  v30_metadata_retry_at=clock_timestamp()+make_interval(secs=>least(300,5*(2^(n-1))::integer))
 WHERE tenant_id=p_tenant AND id=p_task;
 IF n=7 THEN
  IF photo.reject_v2_adoption(p_tenant,p_task,p_result,p_code) IS TRUE THEN RETURN 'rejected';END IF;
  RETURN 'stale';
 END IF;
 RETURN 'deferred';
END $$;
ALTER FUNCTION photo.settle_v30_metadata_invalid(uuid,uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.settle_v30_metadata_invalid(uuid,uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.settle_v30_metadata_invalid(uuid,uuid,integer,bigint,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
