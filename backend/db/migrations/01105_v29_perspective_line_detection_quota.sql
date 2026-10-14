-- +goose Up
-- Bounded source-authorized reservation before document publication.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v29_perspective_line_detection(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant photo.tenants%ROWTYPE; usage photo.tenant_usage%ROWTYPE;
 r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE; plan jsonb;
 budget constant bigint:=4194304;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO tenant FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.kind<>'v2'
 OR r.status<>'draft' OR r.cancel_requested OR r.graph_sealed THEN RETURN false;END IF;
 plan:=r.config_snapshot->'v29_perspective_line_detection';
 -- Reuse full saved-plan/source/snapshot/session checks under the same locks.
 -- JSONB serialization changes the envelope only; original JCS strings and
 -- deterministic task identity seed are retained verbatim.
 IF photo.save_v29_perspective_line_detection_intent(p_tenant,r.created_by,
 (plan->>'auth_session_id')::uuid,plan::text) IS DISTINCT FROM p_run THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations
 WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF FOUND THEN RETURN q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by
 AND q.bytes=budget AND q.files=0 AND q.expires_at>clock_timestamp();END IF;
 IF tenant.quotas->>'storage_bytes' IS NULL
 OR usage.storage_bytes::numeric+usage.reserved_bytes+budget>(tenant.quotas->>'storage_bytes')::numeric
 OR usage.storage_bytes::numeric+usage.reserved_bytes+budget>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',r.created_by,'run',p_run,budget,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+budget WHERE tenant_id=p_tenant;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.reserve_v29_perspective_line_detection(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v29_perspective_line_detection(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.reserve_v29_perspective_line_detection(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
