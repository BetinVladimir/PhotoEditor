-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='photo_gateway') THEN CREATE ROLE photo_gateway NOLOGIN NOBYPASSRLS; END IF; END $$;
-- +goose StatementEnd
GRANT USAGE ON SCHEMA photo TO photo_gateway;
ALTER TABLE photo.worker_registrations ADD COLUMN certificate_sha256 text CHECK(certificate_sha256 ~ '^[0-9a-f]{64}$');
-- Only the internal mTLS gateway receives this privilege. Certificate identity
-- comes from the verified TLS leaf, never a client-supplied certificate header.
-- +goose StatementBegin
CREATE FUNCTION photo.authorize_gateway_task(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text)
RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid,worker_id uuid,attempt integer,fence bigint,lease_until timestamptz,deadline timestamptz,operation text,execution_profile jsonb)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.run_id,t.id,t.worker_id,t.attempt,t.fencing_token,t.lease_until,t.deadline,t.operation,t.execution_profile
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.resource_slots s ON s.tenant_id=t.tenant_id AND s.task_id=t.id AND s.fencing_token=t.fencing_token
 JOIN photo.worker_registrations w ON w.tenant_id=t.tenant_id AND w.worker_id=t.worker_id AND w.node_id=s.node_id
 WHERE t.id=p_task AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.worker_id=p_worker
 AND t.state='running' AND t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp()
 AND NOT t.cancel_requested AND NOT r.cancel_requested AND r.status IN ('queued','running','waiting_review')
 AND w.enabled AND w.certificate_sha256=p_certificate AND p_certificate ~ '^[0-9a-f]{64}$'
 AND w.resource_classes ? t.resource_class AND w.backends ? (t.execution_profile->>'backend')
 AND (w.qualified_operations->t.operation) ? (t.execution_profile->>'backend')
 AND ((t.execution_profile->>'model_lock_id') IS NULL OR w.lock_id=t.execution_profile->>'model_lock_id');
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) TO photo_gateway;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
