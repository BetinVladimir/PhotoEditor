-- +goose Up
-- Keep terminal history out of recovery scans, including JSON ID joins.
CREATE INDEX runs_v26_recovery_pending ON photo.runs(id)
WHERE kind='bulk' AND status='draft' AND NOT cancel_requested AND NOT graph_sealed
 AND config_snapshot->'schema_version'='1'::jsonb AND config_snapshot ? 'parts';
CREATE INDEX runs_v26_recovery_child ON photo.runs(tenant_id,(id::text)) INCLUDE(config_hash)
WHERE kind='v2' AND status='draft' AND NOT cancel_requested AND NOT graph_sealed;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
