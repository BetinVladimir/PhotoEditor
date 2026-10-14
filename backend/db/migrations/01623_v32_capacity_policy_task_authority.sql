-- +goose Up
-- Retain existing principal/operation and V31 signer-pool authority; bind a
-- policy-pinned worker to a recent matching capacity report independently of
-- task heartbeat's last_seen updates.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.worker_authorized(p_tenant uuid,p_worker uuid,p_class text,p_node text,p_operation text,p_backend text,p_lock_id text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.worker_authorized_pre_v31_signer(p_tenant,p_worker,p_class,p_node,p_operation,p_backend,p_lock_id)
 AND EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker
 AND CASE WHEN p_operation='provenance.sign' THEN w.execution_pool='signer' AND w.signer_qualified_until>clock_timestamp()
 ELSE w.execution_pool='general' END
 AND (w.capacity_policy_sha IS NULL OR
  (w.reported_capacity_policy_sha=w.capacity_policy_sha
   AND w.capacity_policy_reported_at>clock_timestamp()-interval '30 seconds'
   AND w.capacity_policy_reported_at<=clock_timestamp())));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.worker_authorized(uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.worker_authorized(uuid,uuid,text,text,text,text,text) FROM PUBLIC,photo_worker,photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
