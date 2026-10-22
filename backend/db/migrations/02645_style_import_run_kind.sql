-- +goose Up
-- Style.Import and Export.Create emit the closed style_import/export_batch kinds,
-- but complete run-kind enumerations omitted them. Preserve all existing kinds
-- and the separate v2 fail-fast fence; admit no arbitrary text kinds.
-- +goose StatementBegin
DO $$
DECLARE definition text;
BEGIN
 SELECT pg_get_constraintdef(oid) INTO STRICT definition FROM pg_constraint
 WHERE conrelid='photo.runs'::regclass AND conname='runs_domain_0' AND contype='c' AND convalidated;
 IF encode(sha256(convert_to(definition,'UTF8')),'hex')<>'53265e674315d585c586ff8f5acd2bd635c77b84183f287ed978f3478ef93bc5' THEN
 RAISE EXCEPTION 'STYLE_IMPORT_RUN_KIND_PREIMAGE_DRIFT';END IF;
 ALTER TABLE photo.runs DROP CONSTRAINT runs_domain_0;
 ALTER TABLE photo.runs ADD CONSTRAINT runs_domain_0 CHECK(kind IN(
  'analyze','apply','cull','preview','export','ingest','download','bulk','maintenance','v2','mask','style_import','export_batch'));
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02645 is forward-only';END $$;
-- +goose StatementEnd
