-- +goose Up
-- Only verified signer adoption may insert. No endpoint/dispatch is activated.
CREATE TABLE photo.provenance_manifests (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 export_id uuid NOT NULL,
 workflow_id uuid NOT NULL,
 manifest_artifact_id uuid NOT NULL,
 signed_artifact_id uuid NOT NULL,
 signer_key_id text NOT NULL CHECK(length(signer_key_id) BETWEEN 1 AND 256),
 signing_policy_sha text NOT NULL CHECK(signing_policy_sha ~ '^[0-9a-f]{64}$'),
 UNIQUE(tenant_id,id),
 UNIQUE(tenant_id,project_id,id),
 UNIQUE(tenant_id,export_id,signing_policy_sha),
 CHECK(manifest_artifact_id<>signed_artifact_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,export_id) REFERENCES photo.exports(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,project_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,manifest_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,signed_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE photo.provenance_manifests ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.provenance_manifests FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.provenance_manifests TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.provenance_manifests FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT ON photo.provenance_manifests TO photo_fn_owner;
CREATE INDEX provenance_manifests_workflow ON photo.provenance_manifests(tenant_id,project_id,workflow_id);
CREATE INDEX provenance_manifests_signed_artifact ON photo.provenance_manifests(tenant_id,signed_artifact_id);
CREATE INDEX provenance_manifests_manifest_artifact ON photo.provenance_manifests(tenant_id,manifest_artifact_id);
CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.provenance_manifests FOR EACH ROW
 EXECUTE FUNCTION photo.reject_content_update('id','tenant_id','project_id','created_at','export_id','workflow_id','manifest_artifact_id','signed_artifact_id','signer_key_id','signing_policy_sha');
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
