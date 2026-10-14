-- +goose Up
-- Mask.Create already emits kind=mask; canonical V28 producer checks require it.
-- Preserve every existing run kind and the separate v2 fail-fast invariant.
ALTER TABLE photo.runs DROP CONSTRAINT runs_domain_0;
ALTER TABLE photo.runs ADD CONSTRAINT runs_domain_0
 CHECK(kind IN('analyze','apply','cull','preview','export','ingest','download','bulk','maintenance','v2','mask'));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
