-- An ambiguous S3 CreateMultipartUpload can outlive the API request. Its open
-- writer row must survive until explicit reconciliation; tenant metadata
-- deletion must not erase that durable physical-IO fence.
-- +goose Up
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text:=' SET CONSTRAINTS ALL DEFERRED;';replacement text;
BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF definition IS NULL OR position(needle IN definition)=0 OR
    position('v30_archive_writers' IN definition)=0 OR
    position('v30_archive_uploads' IN definition)=0 OR
    position('IF EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=j.tenant_id AND w.state=''open'')' IN definition)>0
 THEN RAISE EXCEPTION 'TENANT_FINISH_ARCHIVE_WRITER_SHAPE'; END IF;
 replacement:=' IF EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=j.tenant_id AND w.state=''open'') THEN RETURN false;END IF;'||needle;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
