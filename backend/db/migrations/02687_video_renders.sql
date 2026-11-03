-- +goose Up
-- Video renders (DS-06 and 04 section 2.5). One render per job; ready requires output and QC report.

CREATE TABLE photo.video_renders (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  sequence_id uuid NOT NULL,
  sequence_revision bigint NOT NULL CHECK (sequence_revision > 0),
  document_sha256 text NOT NULL CHECK (document_sha256 ~ '^[0-9a-f]{64}$'),
  preset jsonb NOT NULL CHECK (jsonb_typeof(preset)='object' AND pg_column_size(preset) <= 16384),
  preset_sha256 text NOT NULL CHECK (preset_sha256 ~ '^[0-9a-f]{64}$'),
  job_id uuid NOT NULL,
  output_artifact_id uuid,
  output_bytes bigint CHECK (output_bytes IS NULL OR output_bytes > 0),
  duration_ticks bigint CHECK (duration_ticks IS NULL OR duration_ticks BETWEEN 1 AND 7620480000000),
  width integer NOT NULL CHECK (width BETWEEN 16 AND 4096 AND width % 2 = 0),
  height integer NOT NULL CHECK (height BETWEEN 16 AND 2304 AND height % 2 = 0),
  fps_num integer NOT NULL CHECK (fps_num > 0),
  fps_den integer NOT NULL CHECK (fps_den > 0),
  video_codec text NOT NULL CHECK (video_codec IN ('h264')),
  audio_codec text NOT NULL CHECK (audio_codec IN ('aac')),
  encoder text CHECK (encoder IS NULL OR length(encoder) BETWEEN 1 AND 64),
  backend_used text CHECK (backend_used IS NULL OR length(backend_used) BETWEEN 1 AND 64),
  render_fingerprint text CHECK (render_fingerprint IS NULL OR render_fingerprint ~ '^[0-9a-f]{64}$'),
  qc_report_artifact_id uuid,
  state text NOT NULL CHECK (length(state) BETWEEN 1 AND 256 AND state IN ('queued','rendering','verifying','ready','failed','cancelled','deleted')),
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,job_id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,sequence_id) REFERENCES photo.video_sequences(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,job_id) REFERENCES photo.video_jobs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,output_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,qc_report_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK ((fps_num,fps_den) IN ((24,1),(25,1),(30,1),(50,1),(60,1),(24000,1001),(30000,1001),(60000,1001))),
  CHECK (state <> 'ready' OR (output_artifact_id IS NOT NULL AND qc_report_artifact_id IS NOT NULL AND output_bytes IS NOT NULL AND duration_ticks IS NOT NULL)),
  CHECK (state <> 'deleted' OR deleted_at IS NOT NULL)
);
CREATE INDEX video_renders_project_page ON photo.video_renders(tenant_id,project_id,created_at,id);
CREATE INDEX video_renders_sequence ON photo.video_renders(tenant_id,sequence_id,sequence_revision);

ALTER TABLE photo.video_renders ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_renders FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_renders TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.video_renders FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.video_renders TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
