-- +goose Up
-- Release only residual run quota for cancelled cross-run invalidated consumers.
-- Uploaded/ready artifacts retain storage accounting. In-flight object cleanup
-- and task resources must be settled first. This runs outside record/reconcile
-- transactions to preserve tenant -> usage -> run lock order.
-- +goose StatementBegin
CREATE FUNCTION photo.release_v29_negative_cancelled_quota(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'cancelled' OR NOT r.cancel_requested
 OR NOT EXISTS(SELECT 1 FROM photo.v29_negative_invalidations i
 JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 JOIN photo.tasks origin ON origin.tenant_id=i.tenant_id AND origin.id=i.origin_task_id
 WHERE i.tenant_id=p_tenant AND t.run_id=p_run AND origin.run_id<>p_run) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
 AND (t.state NOT IN('succeeded','failed','cancelled','skipped') OR t.lease_until IS NOT NULL))
 OR EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND u.state NOT IN('ready','cleaned'))
 OR EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.state NOT IN('ready','cleaned'))
 OR EXISTS(SELECT 1 FROM photo.resource_reservations x JOIN photo.tasks t ON t.tenant_id=x.tenant_id AND t.id=x.task_id WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND x.state<>'released')
 OR EXISTS(SELECT 1 FROM photo.resource_slots x JOIN photo.tasks t ON t.tenant_id=x.tenant_id AND t.id=x.task_id WHERE t.tenant_id=p_tenant AND t.run_id=p_run) THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN true;END IF;
 IF q.state='released' AND q.bytes=0 THEN RETURN true;END IF;
 IF q.state<>'reserved' OR q.bytes<0 OR q.files<>0 OR q.principal_type<>'user' OR q.principal_id<>r.created_by THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'NEGATIVE_CANCELLED_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
 UPDATE photo.quota_reservations SET state='released',bytes=0,expires_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=q.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.release_v29_negative_cancelled_quota(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_v29_negative_cancelled_quota(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_v29_negative_cancelled_quota(uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.release_next_v29_negative_cancelled_quota()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;prior text;released boolean;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
 WHERE r.kind='v2' AND r.status='cancelled' AND r.cancel_requested AND q.state='reserved' AND q.files=0
 AND EXISTS(SELECT 1 FROM photo.v29_negative_invalidations i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 JOIN photo.tasks origin ON origin.tenant_id=i.tenant_id AND origin.id=i.origin_task_id
 WHERE i.tenant_id=r.tenant_id AND t.run_id=r.id AND origin.run_id<>r.id)
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
 AND (t.state NOT IN('succeeded','failed','cancelled','skipped') OR t.lease_until IS NOT NULL))
 AND NOT EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND u.state NOT IN('ready','cleaned'))
 AND NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.state NOT IN('ready','cleaned'))
 AND NOT EXISTS(SELECT 1 FROM photo.resource_reservations x JOIN photo.tasks t ON t.tenant_id=x.tenant_id AND t.id=x.task_id WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND x.state<>'released')
 AND NOT EXISTS(SELECT 1 FROM photo.resource_slots x JOIN photo.tasks t ON t.tenant_id=x.tenant_id AND t.id=x.task_id WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id)
 ORDER BY r.created_at,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 prior:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',candidate.tenant_id::text,true);
 released:=photo.release_v29_negative_cancelled_quota(candidate.tenant_id,candidate.id);
 PERFORM set_config('app.tenant_id',coalesce(prior,''),true);
 RETURN released;
END $$;
ALTER FUNCTION photo.release_next_v29_negative_cancelled_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_next_v29_negative_cancelled_quota() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_next_v29_negative_cancelled_quota() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
