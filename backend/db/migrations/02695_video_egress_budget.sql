-- +goose Up
-- Video stream egress counters, period/share budgets and leases (DS-28). Written only by FN-D-19/20/22.

CREATE TABLE photo.video_egress_counters (        -- поток, агрегат по часу
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  share_id uuid NOT NULL, render_id uuid NOT NULL,
  kind text NOT NULL CHECK (kind = 'stream'),
  bucket_start timestamptz NOT NULL CHECK (bucket_start = date_trunc('hour', bucket_start)),
  bytes bigint NOT NULL DEFAULT 0 CHECK (bytes >= 0),
  requests bigint NOT NULL DEFAULT 0 CHECK (requests >= 0),
  PRIMARY KEY (tenant_id, share_id, render_id, kind, bucket_start),
  FOREIGN KEY (tenant_id,render_id) REFERENCES photo.video_renders(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE photo.video_egress_period (          -- общий лимит арендатора за месяц UTC
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  period_start date NOT NULL CHECK (period_start = date_trunc('month', period_start)::date),
  consumed_bytes bigint NOT NULL DEFAULT 0 CHECK (consumed_bytes >= 0),
  leased_bytes bigint NOT NULL DEFAULT 0 CHECK (leased_bytes >= 0),
  limit_bytes bigint NOT NULL CHECK (limit_bytes >= 0),
  PRIMARY KEY (tenant_id, period_start),
  CHECK (consumed_bytes + leased_bytes <= limit_bytes)
);
CREATE TABLE photo.video_share_budget (           -- лимит на ссылку; строка есть только если у ссылки задан лимит
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  share_id uuid NOT NULL,
  consumed_bytes bigint NOT NULL DEFAULT 0 CHECK (consumed_bytes >= 0),
  leased_bytes bigint NOT NULL DEFAULT 0 CHECK (leased_bytes >= 0),
  limit_bytes bigint NOT NULL CHECK (limit_bytes >= 0),
  PRIMARY KEY (tenant_id, share_id),
  CHECK (consumed_bytes + leased_bytes <= limit_bytes)
);
CREATE TABLE photo.video_egress_leases (
  id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  share_id uuid NOT NULL,
  period_start date NOT NULL,
  granted_bytes bigint NOT NULL CHECK (granted_bytes > 0),
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id)
);
CREATE INDEX video_egress_leases_expiry ON photo.video_egress_leases(expires_at);

ALTER TABLE photo.video_egress_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_egress_counters FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_egress_counters TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_egress_period ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_egress_period FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_egress_period TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_share_budget ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_share_budget FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_share_budget TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_egress_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_egress_leases FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_egress_leases TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.video_egress_counters,photo.video_egress_period,photo.video_share_budget,photo.video_egress_leases FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.video_egress_counters,photo.video_egress_period,photo.video_share_budget,photo.video_egress_leases TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
