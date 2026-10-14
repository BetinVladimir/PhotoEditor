-- +goose Up
-- Operator activation is explicit. The Go scheduler refreshes a short-lived
-- adopter heartbeat only after its physical verifier dependencies are ready.
CREATE TABLE photo.v23_identity_adopter_state (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 last_seen timestamptz NOT NULL
);
REVOKE ALL ON photo.v23_identity_adopter_state FROM PUBLIC,photo_api,
 photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT SELECT,INSERT,UPDATE ON photo.v23_identity_adopter_state TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_identity_adopter_heartbeat() RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 INSERT INTO photo.v23_identity_adopter_state(singleton,last_seen)
 VALUES(true,clock_timestamp()) ON CONFLICT(singleton) DO UPDATE
 SET last_seen=excluded.last_seen;
 RETURN true;
END $$;
ALTER FUNCTION photo.v23_identity_adopter_heartbeat() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_identity_adopter_heartbeat()
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v23_identity_adopter_heartbeat() TO photo_scheduler;
-- +goose StatementEnd

GRANT UPDATE(v2_enabled) ON photo.operation_registry TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.configure_v23_identity_dispatch(p_enable boolean,p_policy uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
DECLARE changed integer;
BEGIN
 IF p_enable IS NULL THEN RETURN false;END IF;
 IF p_enable THEN
  IF p_policy IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v23_identity_adopter_state a
   WHERE a.singleton AND a.last_seen>clock_timestamp()-interval '30 seconds')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_qualification_policies policy
    JOIN photo.model_qualifications qualified ON
     qualified.operation=policy.operation AND
     qualified.operation_version=policy.operation_version AND
     qualified.model_lock_sha=policy.model_lock_sha AND
     qualified.runtime_lock_sha=policy.runtime_lock_sha AND
     qualified.device_profile_sha=policy.device_profile_sha AND
     qualified.dataset_sha=policy.dataset_sha AND
     qualified.backend=policy.backend AND qualified.trusted_import AND
     qualified.status='passed'
    JOIN photo.tenants tenant ON tenant.id=qualified.tenant_id AND tenant.status='active'
    JOIN photo.projects project ON project.tenant_id=qualified.tenant_id AND
     project.id=qualified.project_id AND project.archived_at IS NULL
    JOIN photo.artifacts report ON report.tenant_id=qualified.tenant_id AND
     report.id=qualified.report_artifact_id AND report.sha256=qualified.report_sha AND
     report.state='ready' AND report.media_type='application/json' AND
     report.bytes BETWEEN 2 AND 8388608 AND report.verified_at IS NOT NULL AND
     report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND
     (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
    WHERE policy.id=p_policy AND policy.operation='identity.compare' AND
     policy.operation_version='1' AND policy.backend IN('cpu','cuda') AND
     policy.enabled AND policy.admission_enabled AND policy.license_cleared AND
     cardinality(policy.calibration_shas)>0 AND
     EXISTS(SELECT 1 FROM photo.worker_registrations worker WHERE
      worker.tenant_id=qualified.tenant_id AND worker.enabled AND
      worker.last_seen>clock_timestamp()-interval '30 seconds' AND
      worker.resource_classes ? 'gpu' AND worker.backends ? policy.backend AND
      (worker.qualified_operations->'identity.compare') ? policy.backend AND
      worker.lock_id=policy.runtime_lock_sha)) THEN RETURN false;END IF;
 END IF;
 UPDATE photo.operation_registry SET v2_enabled=p_enable
 WHERE operation='identity.compare' AND version='1' AND
  (NOT p_enable OR (resource_class='gpu' AND
   routing_key='gpu.identity_compare' AND timeout_seconds=1800 AND
   model_id IS NULL AND backends='["cuda","cpu"]'::jsonb));
 GET DIAGNOSTICS changed=ROW_COUNT;
 RETURN changed=1;
END $$;
ALTER FUNCTION photo.configure_v23_identity_dispatch(boolean,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.configure_v23_identity_dispatch(boolean,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.configure_v23_identity_dispatch(boolean,uuid)
 TO photo_maintenance;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
