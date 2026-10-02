-- Analysis-only latest schema subset for sqlc. Runtime schema is owned by goose.
CREATE SCHEMA photo;
CREATE TABLE photo.export_presets (
 id uuid PRIMARY KEY,
 created_at timestamptz NOT NULL,
 tenant_id uuid NOT NULL,
 name text NOT NULL,
 revision bigint NOT NULL,
 config jsonb NOT NULL,
 config_hash text NOT NULL,
 archived_at timestamptz
);
