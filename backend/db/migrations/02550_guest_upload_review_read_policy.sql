-- +goose Up
-- Owner review reads the exact ingest membership binding under tenant RLS.
-- Mutation and cross-tenant access remain unavailable to the API role.
CREATE POLICY tenant_review_read ON photo.ingest_upload_bindings
 FOR SELECT TO photo_api
 USING (tenant_id = nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT SELECT ON photo.ingest_upload_bindings TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
