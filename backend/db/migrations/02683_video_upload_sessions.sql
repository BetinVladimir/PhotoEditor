-- +goose Up
-- Video upload sessions (DS-02) and part metadata. Direct DML is closed for every
-- application role; later SECURITY DEFINER functions (02690) are the only entry point.
-- Limits follow confirmed decision Q-02: 16 GiB per source file.
CREATE TABLE photo.video_upload_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  principal_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  media_kind text NOT NULL CHECK (media_kind IN ('video','audio','lut')),
  filename text NOT NULL CHECK (length(filename) BETWEEN 1 AND 255 AND filename !~ '[/\\\x00]'),
  declared_bytes bigint NOT NULL CHECK (declared_bytes BETWEEN 1 AND 17179869184),
  declared_sha256 text NOT NULL CHECK (declared_sha256 ~ '^[0-9a-f]{64}$'),
  request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
  part_bytes bigint NOT NULL CHECK (part_bytes >= 33554432),
  parts_total integer NOT NULL CHECK (parts_total BETWEEN 1 AND 10000),
  multipart_id text,
  object_key text NOT NULL CHECK (length(object_key) BETWEEN 1 AND 1024),
  object_version text CHECK (object_version IS NULL OR length(object_version) BETWEEN 1 AND 1024),
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256 AND state IN
    ('initializing','uploading','completing','verifying','verified','rejected','aborted','expired')),
  reject_code text CHECK (reject_code IS NULL OR length(reject_code) BETWEEN 1 AND 64),
  reservation_id uuid,
  completion_token uuid,
  completion_lease_until timestamptz,
  expires_at timestamptz NOT NULL,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,principal_id,request_sha256),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK (parts_total = (declared_bytes + part_bytes - 1) / part_bytes),
  CHECK (state <> 'rejected' OR reject_code IS NOT NULL)
);
CREATE INDEX video_upload_sessions_project_page ON photo.video_upload_sessions(tenant_id,project_id,created_at,id);
CREATE INDEX video_upload_sessions_expiry ON photo.video_upload_sessions(expires_at) WHERE state IN ('initializing','uploading','completing');

CREATE TABLE photo.video_upload_parts (
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  upload_id uuid NOT NULL,
  part_no integer NOT NULL CHECK (part_no BETWEEN 1 AND 10000),
  etag text NOT NULL CHECK (length(etag) BETWEEN 1 AND 256),
  bytes bigint NOT NULL CHECK (bytes > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,upload_id,part_no),
  FOREIGN KEY (tenant_id,upload_id) REFERENCES photo.video_upload_sessions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);

ALTER TABLE photo.video_upload_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_upload_sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE photo.video_upload_parts ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_upload_parts FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_upload_sessions TO photo_fn_owner USING (true) WITH CHECK (true);
CREATE POLICY trusted_functions ON photo.video_upload_parts TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.video_upload_sessions,photo.video_upload_parts FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.video_upload_sessions,photo.video_upload_parts TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
