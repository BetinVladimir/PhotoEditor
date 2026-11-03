-- +goose Up
-- Video delivery (DS-07): collection kind, collection items, download egress events (partitioned by month).
-- Direct DML closed; written only by SECURITY DEFINER functions (02692/02695).

ALTER TABLE photo.collections ADD COLUMN content_kind text NOT NULL DEFAULT 'photo'
  CHECK (content_kind IN ('photo','video'));
CREATE TABLE photo.video_collection_items (
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  collection_id uuid NOT NULL,
  render_id uuid NOT NULL,
  ordinal integer NOT NULL CHECK (ordinal BETWEEN 0 AND 999),
  title text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
  published_at timestamptz,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  PRIMARY KEY (tenant_id,collection_id,render_id),
  UNIQUE (tenant_id,collection_id,ordinal),
  FOREIGN KEY (tenant_id,collection_id) REFERENCES photo.collections(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,render_id) REFERENCES photo.video_renders(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
-- только СКАЧИВАНИЯ (одна строка на завершённое или прерванное скачивание); поток учитывается в DS-28
CREATE TABLE photo.video_egress_events (
  id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  share_id uuid,
  render_id uuid NOT NULL,
  kind text NOT NULL CHECK (kind = 'download'),
  bytes bigint NOT NULL CHECK (bytes >= 0),
  at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id, at),
  FOREIGN KEY (tenant_id,render_id) REFERENCES photo.video_renders(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
) PARTITION BY RANGE (at);
CREATE TABLE photo.video_egress_events_default PARTITION OF photo.video_egress_events DEFAULT;  -- месячные партиции создаёт FN-D-21
CREATE INDEX video_egress_events_period ON photo.video_egress_events(tenant_id,at);

ALTER TABLE photo.video_collection_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_collection_items FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_collection_items TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_egress_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_egress_events FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_egress_events TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_egress_events_default ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_egress_events_default FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_egress_events_default TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.video_collection_items,photo.video_egress_events,photo.video_egress_events_default FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.video_collection_items,photo.video_egress_events TO photo_fn_owner;
-- Partitions are reached only through the parent table: no direct grant on the default partition.

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
