-- +goose Up
-- A generation is a separately fenced S3 object key for one Create attempt.
-- This migration does not infer receipts for legacy open Begin writers and does
-- not switch the active upload/part/completion paths to generation keys.
CREATE TABLE photo.v30_archive_begin_generations (
 tenant_id uuid NOT NULL,
 upload_id uuid NOT NULL,
 generation_id uuid NOT NULL,
 writer_token uuid NOT NULL,
 reservation_token uuid NOT NULL,
 creator_user_id uuid NOT NULL,
 creator_session_id uuid NOT NULL,
 request_sha text NOT NULL CHECK (request_sha ~ '^[0-9a-f]{64}$'),
 object_key text NOT NULL,
 state text NOT NULL DEFAULT 'creating' CHECK (state IN ('creating','created','create_failed','aborting','aborted','bound')),
 multipart_id text,
 terminal_evidence text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 terminal_at timestamptz,
 revision bigint NOT NULL DEFAULT 1 CHECK (revision BETWEEN 1 AND 9007199254740991),
 PRIMARY KEY (tenant_id,upload_id,generation_id),
 UNIQUE (tenant_id,object_key),
 UNIQUE (tenant_id,upload_id,writer_token),
 FOREIGN KEY (tenant_id,upload_id) REFERENCES photo.v30_archive_uploads(tenant_id,id) DEFERRABLE INITIALLY IMMEDIATE,
 FOREIGN KEY (tenant_id,upload_id,writer_token) REFERENCES photo.v30_archive_writers(tenant_id,upload_id,token) DEFERRABLE INITIALLY IMMEDIATE,
 CHECK (object_key='tenants/'||tenant_id||'/interchange-uploads/'||upload_id||'/generations/'||generation_id||'/archive.zip'),
 CHECK (multipart_id IS NULL OR (length(multipart_id) BETWEEN 1 AND 1024 AND multipart_id !~ '[[:cntrl:]]')),
 CHECK ((state IN ('created','aborting','bound') AND multipart_id IS NOT NULL) OR state NOT IN ('created','aborting','bound')),
 CHECK (state NOT IN ('creating','create_failed') OR multipart_id IS NULL),
 CHECK ((state='creating' AND terminal_evidence IS NULL AND terminal_at IS NULL)
     OR (state<>'creating' AND terminal_evidence IS NOT NULL AND length(terminal_evidence) BETWEEN 1 AND 2048 AND terminal_at IS NOT NULL))
);
CREATE UNIQUE INDEX v30_archive_begin_generations_live_one
 ON photo.v30_archive_begin_generations(tenant_id,upload_id)
 WHERE state IN ('creating','created','aborting','bound');
CREATE INDEX v30_archive_begin_generations_tenant_state
 ON photo.v30_archive_begin_generations(tenant_id,state,upload_id);
ALTER TABLE photo.v30_archive_begin_generations ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_archive_begin_generations FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_archive_begin_generations TO photo_fn_owner USING (true) WITH CHECK (true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.v30_archive_begin_generations TO photo_fn_owner;
REVOKE ALL ON photo.v30_archive_begin_generations FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

-- Add the child table to the explicit purge allowlist before its parent. Purge
-- remains fenced by both the old writer guard and this new generation guard.
-- +goose StatementBegin
DO $$ DECLARE definition text; needle text:=' SET CONSTRAINTS ALL DEFERRED;';
BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF definition IS NULL OR position('tables_to_purge constant text[]:=ARRAY[' IN definition)=0
    OR position(needle IN definition)=0 OR position('v30_archive_writers' IN definition)=0
    OR position('v30_archive_begin_generations' IN definition)>0
 THEN RAISE EXCEPTION 'TENANT_FINISH_ARCHIVE_GENERATION_SHAPE'; END IF;
 definition:=replace(definition,'tables_to_purge constant text[]:=ARRAY[',
   'tables_to_purge constant text[]:=ARRAY[''v30_archive_begin_generations'',');
 definition:=replace(definition,needle,
   ' IF EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=j.tenant_id AND g.state IN(''creating'',''created'',''aborting'',''bound'')) THEN RETURN false;END IF;'||needle);
 EXECUTE definition;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
