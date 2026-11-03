-- +goose Up
-- Video jobs and outputs (DS-05). Direct DML closed; created by 02693 functions.

CREATE TABLE photo.video_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  kind text NOT NULL CHECK (kind IN ('ingest','detect_cuts','color_analyze','preview_frames','audio_analyze','render')),
  run_id uuid,                                                          -- NULL: video jobs use the dedicated video queue (claim_video_job), not photo runs
  media_id uuid,
  sequence_id uuid,
  sequence_revision bigint,
  params_artifact_id uuid NOT NULL,
  params_sha256 text NOT NULL CHECK (params_sha256 ~ '^[0-9a-f]{64}$'),
  state text NOT NULL CHECK (state IN ('queued','running','succeeded','failed','cancelled','expired')),
  stage text CHECK (stage IS NULL OR length(stage) BETWEEN 1 AND 64),
  progress numeric(5,4) NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 1),
  eta_seconds integer CHECK (eta_seconds IS NULL OR eta_seconds >= 0),
  backend_requested text NOT NULL CHECK (backend_requested IN ('auto','cuda','mlx','cpu')),
  backend_used text CHECK (backend_used IS NULL OR backend_used IN ('cuda','mlx','mps','cpu')),
  reservation_id uuid,
  cost_seconds integer NOT NULL DEFAULT 0 CHECK (cost_seconds >= 0),   -- FN-Q-09, считается один раз при создании
  pool text NOT NULL CHECK (pool IN ('video_light','video_heavy')),     -- учётный пул: audio_analyze, preview_frames → video_light; остальное → video_heavy
  error_code text CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),
  error_detail text CHECK (error_detail IS NULL OR length(error_detail) <= 512),
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  finished_at timestamptz,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  request_sha256 text CHECK (request_sha256 IS NULL OR request_sha256 ~ '^[0-9a-f]{64}$'),   -- idempotent create
  claim_token uuid,
  lease_until timestamptz,
  attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  cancel_requested boolean NOT NULL DEFAULT false,
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,run_id),
  UNIQUE (tenant_id,created_by,request_sha256),
  CHECK ((claim_token IS NULL) = (lease_until IS NULL)),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK (state <> 'failed' OR error_code IS NOT NULL),
  CHECK (kind NOT IN ('detect_cuts','color_analyze','audio_analyze') OR media_id IS NOT NULL),
  CHECK (kind NOT IN ('preview_frames','render') OR (sequence_id IS NOT NULL AND sequence_revision IS NOT NULL)),
  CHECK ((kind IN ('audio_analyze','preview_frames')) = (pool = 'video_light'))
) WITH (fillfactor = 70);   -- обновления прогресса остаются «горячими» (VID-SC-06)
-- допуск (FN-D-17) считает находящиеся в системе задания и стоимость очереди по этим индексам:
CREATE INDEX video_jobs_outstanding ON photo.video_jobs(tenant_id, kind) WHERE state IN ('queued','running');
CREATE INDEX video_jobs_pool_queued ON photo.video_jobs(pool) INCLUDE (cost_seconds) WHERE state = 'queued';
-- запрещены индексы по progress и eta_seconds (VID-SC-06).
CREATE TABLE photo.video_job_outputs (
  tenant_id uuid NOT NULL, job_id uuid NOT NULL,
  role text NOT NULL CHECK (length(role) BETWEEN 1 AND 64),
  ordinal integer NOT NULL CHECK (ordinal BETWEEN 0 AND 127),
  artifact_id uuid NOT NULL,
  PRIMARY KEY (tenant_id,job_id,role,ordinal),
  FOREIGN KEY (tenant_id,job_id) REFERENCES photo.video_jobs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);

ALTER TABLE photo.video_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_jobs TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_job_outputs ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_job_outputs FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_job_outputs TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.video_jobs,photo.video_job_outputs FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.video_jobs,photo.video_job_outputs TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
