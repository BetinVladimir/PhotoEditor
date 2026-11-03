-- +goose Up
-- Video media (DS-03). Direct DML closed; written only by SECURITY DEFINER functions (02691).

CREATE TABLE photo.video_media (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  kind text NOT NULL CHECK (kind IN ('video','audio','lut')),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
  state text NOT NULL CHECK (state IN ('probing','ready','rejected','failed','withdrawing','deleted')),
  reject_code text CHECK (reject_code IS NULL OR length(reject_code) BETWEEN 1 AND 64),
  upload_id uuid NOT NULL,
  ingest_job_id uuid,
  source_artifact_id uuid NOT NULL,
  source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
  source_bytes bigint NOT NULL CHECK (source_bytes > 0),
  probe_artifact_id uuid,
  probe jsonb CHECK (probe IS NULL OR (jsonb_typeof(probe)='object' AND pg_column_size(probe) <= 16384)),
  proxy_artifact_id uuid,
  filmstrip_artifact_id uuid,
  waveform_artifact_id uuid,
  withdrawn_at timestamptz,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,source_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,upload_id) REFERENCES photo.video_upload_sessions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK (state <> 'rejected' OR reject_code IS NOT NULL),
  CHECK (state <> 'ready' OR (probe_artifact_id IS NOT NULL AND probe IS NOT NULL AND
        ((kind='video'  AND proxy_artifact_id IS NOT NULL AND filmstrip_artifact_id IS NOT NULL AND waveform_artifact_id IS NOT NULL) OR
         (kind='audio'  AND waveform_artifact_id IS NOT NULL AND proxy_artifact_id IS NULL AND filmstrip_artifact_id IS NULL) OR
         (kind='lut'    AND proxy_artifact_id IS NULL AND filmstrip_artifact_id IS NULL AND waveform_artifact_id IS NULL)))),
  CHECK (state <> 'deleted' OR withdrawn_at IS NOT NULL)
);
CREATE INDEX video_media_project_page ON photo.video_media(tenant_id,project_id,created_at,id) WHERE state <> 'deleted';

ALTER TABLE photo.video_media ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_media FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_media TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.video_media FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.video_media TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
