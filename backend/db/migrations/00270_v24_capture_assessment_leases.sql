-- +goose Up
ALTER TABLE photo.capture_assessment_jobs ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_capture_assessment()
RETURNS SETOF photo.capture_assessment_jobs
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.capture_assessment_jobs%ROWTYPE; claimed photo.capture_assessment_jobs%ROWTYPE;
BEGIN
 SELECT j.* INTO hint FROM photo.capture_assessment_jobs j JOIN photo.tenants t ON t.id=j.tenant_id
 WHERE t.status='active' AND (j.state='pending' AND j.next_attempt_at<=clock_timestamp() OR j.state='leased' AND j.lease_until<=clock_timestamp())
 ORDER BY j.created_at,j.id LIMIT 1;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=hint.tenant_id AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO claimed FROM photo.capture_assessment_jobs WHERE tenant_id=hint.tenant_id AND id=hint.id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR NOT(claimed.state='pending' AND claimed.next_attempt_at<=clock_timestamp() OR claimed.state='leased' AND claimed.lease_until<=clock_timestamp()) THEN RETURN; END IF;
 UPDATE photo.capture_assessment_jobs SET state='leased',lease_token=gen_random_uuid(),
  lease_until=clock_timestamp()+interval '60 seconds',attempts=attempts+1
 WHERE tenant_id=claimed.tenant_id AND id=claimed.id RETURNING * INTO claimed;
 RETURN NEXT claimed;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_capture_assessment() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_capture_assessment() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_capture_assessment() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.renew_capture_assessment(p_job uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant uuid;
BEGIN
 
 SELECT tenant_id INTO tenant FROM photo.capture_assessment_jobs WHERE id=p_job;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM 1 FROM photo.capture_assessment_jobs WHERE tenant_id=tenant AND id=p_job FOR UPDATE;
 UPDATE photo.capture_assessment_jobs SET lease_until=clock_timestamp()+interval '60 seconds'
 WHERE tenant_id=tenant AND id=p_job AND state='leased' AND lease_token=p_token AND lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_capture_assessment(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_capture_assessment(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_capture_assessment(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.release_capture_assessment(p_job uuid,p_token uuid,p_delay integer) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant uuid;
BEGIN
 IF p_delay NOT BETWEEN 1 AND 300 THEN RETURN false; END IF;
 SELECT tenant_id INTO tenant FROM photo.capture_assessment_jobs WHERE id=p_job;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM 1 FROM photo.capture_assessment_jobs WHERE tenant_id=tenant AND id=p_job FOR UPDATE;
 UPDATE photo.capture_assessment_jobs SET state='pending',lease_token=NULL,lease_until=NULL,next_attempt_at=clock_timestamp()+make_interval(secs=>p_delay)
 WHERE tenant_id=tenant AND id=p_job AND state='leased' AND lease_token=p_token AND lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_capture_assessment(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_capture_assessment(uuid,uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_capture_assessment(uuid,uuid,integer) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.fail_capture_assessment(p_job uuid,p_token uuid,p_code text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant uuid;
BEGIN
 IF p_code IS NULL OR p_code !~ '^[A-Z][A-Z0-9_]{0,99}$' THEN RETURN false; END IF;
 SELECT tenant_id INTO tenant FROM photo.capture_assessment_jobs WHERE id=p_job;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM 1 FROM photo.capture_assessment_jobs WHERE tenant_id=tenant AND id=p_job FOR UPDATE;
 UPDATE photo.capture_assessment_jobs SET state='failed',lease_token=NULL,lease_until=NULL,error_code=p_code
 WHERE tenant_id=tenant AND id=p_job AND state='leased' AND lease_token=p_token AND lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_capture_assessment(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_capture_assessment(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.fail_capture_assessment(uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
