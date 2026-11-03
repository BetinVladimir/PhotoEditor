-- +goose Up
-- Video sequences, immutable revisions and source usage index (DS-04).

CREATE TABLE photo.video_sequences (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  project_id uuid NOT NULL,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  fps_num integer NOT NULL CHECK (fps_num > 0),
  fps_den integer NOT NULL CHECK (fps_den > 0),
  width integer NOT NULL CHECK (width BETWEEN 16 AND 4096 AND width % 2 = 0),
  height integer NOT NULL CHECK (height BETWEEN 16 AND 2304 AND height % 2 = 0),
  head_revision bigint NOT NULL DEFAULT 0 CHECK (head_revision >= 0),
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  archived_at timestamptz,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  UNIQUE (tenant_id,id),
  UNIQUE (tenant_id,project_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  CHECK ((fps_num,fps_den) IN ((24,1),(25,1),(30,1),(50,1),(60,1),(24000,1001),(30000,1001),(60000,1001)))
);
CREATE TABLE photo.video_sequence_revisions (
  tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
  sequence_id uuid NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  parent_revision bigint CHECK (parent_revision IS NULL OR parent_revision = revision - 1),
  document_artifact_id uuid NOT NULL,
  document_sha256 text NOT NULL CHECK (document_sha256 ~ '^[0-9a-f]{64}$'),
  summary jsonb NOT NULL CHECK (jsonb_typeof(summary)='object' AND pg_column_size(summary) <= 16384),
  label text NOT NULL CHECK (label IN ('autosave','manual','render','cut_application')),
  created_by uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,sequence_id,revision),
  FOREIGN KEY (tenant_id,sequence_id) REFERENCES photo.video_sequences(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tenant_id,document_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE photo.video_sequence_sources (
  tenant_id uuid NOT NULL,
  sequence_id uuid NOT NULL,
  revision bigint NOT NULL,
  source_kind text NOT NULL CHECK (source_kind IN ('video','audio','photo','lut')),
  source_id uuid NOT NULL,
  source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
  PRIMARY KEY (tenant_id,sequence_id,revision,source_kind,source_id),
  FOREIGN KEY (tenant_id,sequence_id,revision) REFERENCES photo.video_sequence_revisions(tenant_id,sequence_id,revision) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX video_sequence_sources_usage ON photo.video_sequence_sources(tenant_id,source_kind,source_id);

-- Sequence revisions are immutable; only the autosave compaction function may delete them
-- (it sets the transaction-local flag photo.video_gc='on'), the tenant purge deletes all of them
-- (photo.video_gc='purge'), nobody may update them.
-- +goose StatementBegin
CREATE FUNCTION photo.video_sequence_revisions_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'VIDEO_REVISION_IMMUTABLE' USING ERRCODE='42501'; END IF;
 IF current_setting('photo.video_gc',true)='purge' THEN RETURN OLD; END IF;
 IF current_setting('photo.video_gc',true) IS DISTINCT FROM 'on' OR OLD.label<>'autosave' THEN
  RAISE EXCEPTION 'VIDEO_REVISION_IMMUTABLE' USING ERRCODE='42501'; END IF;
 RETURN OLD;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.video_sequence_revisions_guard() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.video_sequence_revisions_guard() FROM PUBLIC;
CREATE TRIGGER video_sequence_revisions_immutable BEFORE UPDATE OR DELETE ON photo.video_sequence_revisions
 FOR EACH ROW EXECUTE FUNCTION photo.video_sequence_revisions_guard();

ALTER TABLE photo.video_sequences ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_sequences FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_sequences TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_sequence_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_sequence_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_sequence_revisions TO photo_fn_owner USING (true) WITH CHECK (true);
ALTER TABLE photo.video_sequence_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.video_sequence_sources FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.video_sequence_sources TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.video_sequences,photo.video_sequence_revisions,photo.video_sequence_sources FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,photo_maintenance,photo_gateway;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.video_sequences,photo.video_sequence_revisions,photo.video_sequence_sources TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
