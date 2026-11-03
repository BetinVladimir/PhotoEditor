-- +goose Up
-- Per-tenant video limit overrides, daily usage and upload-begin rate windows (DS-29).

CREATE TABLE photo.video_tenant_limits (          -- строки только для переопределений; нет строки = значения DS-01
  tenant_id uuid PRIMARY KEY REFERENCES photo.tenants(id),
  max_outstanding_ingest integer CHECK (max_outstanding_ingest BETWEEN 0 AND 1000),
  max_outstanding_heavy  integer CHECK (max_outstanding_heavy  BETWEEN 0 AND 1000),
  max_outstanding_light  integer CHECK (max_outstanding_light  BETWEEN 0 AND 1000),
  max_outstanding_preview integer CHECK (max_outstanding_preview BETWEEN 0 AND 1000),
  daily_cost_seconds bigint CHECK (daily_cost_seconds IS NULL OR daily_cost_seconds >= 0),
  daily_cost_unlimited boolean NOT NULL DEFAULT false,   -- true => без суточного ограничения (NULL в daily_cost_seconds означает «значение по умолчанию»)
  max_active_uploads integer CHECK (max_active_uploads BETWEEN 0 AND 100),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE photo.video_tenant_usage_daily (
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  day date NOT NULL,                               -- UTC
  cost_seconds bigint NOT NULL DEFAULT 0 CHECK (cost_seconds >= 0),
  jobs_admitted integer NOT NULL DEFAULT 0 CHECK (jobs_admitted >= 0),
  PRIMARY KEY (tenant_id, day)
);
CREATE TABLE photo.video_tenant_rate (            -- окна для UPLOAD_BEGIN_RATE: один ряд на (арендатор, час)
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  hour_start timestamptz NOT NULL CHECK (hour_start = date_trunc('hour', hour_start)),
  upload_begins integer NOT NULL DEFAULT 0 CHECK (upload_begins >= 0),
  PRIMARY KEY (tenant_id, hour_start)
);

ALTER TABLE photo.video_tenant_limits ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_tenant_limits FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_tenant_limits TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_tenant_usage_daily ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_tenant_usage_daily FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_tenant_usage_daily TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_tenant_rate ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_tenant_rate FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_tenant_rate TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.video_tenant_limits,photo.video_tenant_usage_daily,photo.video_tenant_rate FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.video_tenant_limits,photo.video_tenant_usage_daily,photo.video_tenant_rate TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
