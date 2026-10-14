-- +goose Up
-- The physical purge processor checks authority before/after storage IO. Repeat
-- its ledger predicate at the quota commit under the upload row lock as well.
-- This does not replace independently installed physical writer drain evidence.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 SELECT pg_get_functiondef('photo.finish_v30_archive_cleanup(uuid,uuid,uuid,text,text)'::regprocedure) INTO definition;
 needle:=' UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.expected_bytes';
 IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'ARCHIVE_CLEANUP_AUTHORITY_PATCH_MISMATCH';END IF;
 replacement:=' IF photo.v30_archive_cleanup_authorized(p_tenant,p_upload,p_token,p_bucket,p_key) IS DISTINCT FROM true THEN RETURN false;END IF;
'||needle;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
