-- +goose Up
-- PostgreSQL row-locking SELECT requires UPDATE on at least one column.
-- Only the function owner receives that prerequisite; the immutable trigger
-- refuses all actual UPDATEs, including no-op updates. Tenant purge DELETE stays
-- under its existing explicit allowlist and object-cleanup preconditions.
GRANT UPDATE(document_id) ON photo.semantic_native_protections TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.guard_semantic_native_protection_update() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 RAISE EXCEPTION 'SEMANTIC_NATIVE_PROTECTION_IMMUTABLE' USING ERRCODE='42501';
END $$;
REVOKE ALL ON FUNCTION photo.guard_semantic_native_protection_update() FROM PUBLIC;
CREATE TRIGGER semantic_native_protection_no_update BEFORE UPDATE ON photo.semantic_native_protections
FOR EACH ROW EXECUTE FUNCTION photo.guard_semantic_native_protection_update();
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
