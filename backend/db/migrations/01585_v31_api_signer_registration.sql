-- +goose Up
-- Read-only API observation of a tenant's explicitly provisioned signer.
-- No native trust/cryptographic attestation or qualification renewal is granted.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_api_signer_registration_ready(p_tenant uuid,p_worker uuid,p_sdk text,p_certificate text,p_trust text,p_qualification text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_tenant IS NOT NULL AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND EXISTS(SELECT 1 FROM photo.worker_registrations w
 WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker AND w.enabled
 AND w.execution_pool='signer' AND w.signer_qualified_until>clock_timestamp()
 AND w.last_seen>clock_timestamp()-interval '30 seconds'
 AND w.resource_classes='["cpu"]'::jsonb AND w.backends='["cpu"]'::jsonb
 AND w.qualified_operations='{"provenance.sign":["cpu"]}'::jsonb
 AND w.signer_sdk_lock_sha=p_sdk AND w.signer_certificate_chain_sha=p_certificate
 AND w.signer_trust_bundle_sha=p_trust AND w.signer_qualification_sha=p_qualification),false);
$$;
ALTER FUNCTION photo.v31_api_signer_registration_ready(uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_api_signer_registration_ready(uuid,uuid,text,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v31_api_signer_registration_ready(uuid,uuid,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
