-- +goose Up
-- Final publication must consume the same concrete receipt observed by the
-- writer coordinator. Read-only SHA verification alone is not a write receipt.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 SELECT pg_get_functiondef('photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text)'::regprocedure) INTO definition;
 needle:=' INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)';
 IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'ARCHIVE_PUBLICATION_PATCH_MISMATCH';END IF;
 replacement:=' IF EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=p_tenant AND w.upload_id=p_upload AND w.state=''open'')
 OR NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=p_tenant AND w.upload_id=p_upload AND w.kind=''complete'' AND w.state=''closed'' AND w.receipt=p_version)
 THEN RETURN false;END IF;
'||needle;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
