-- +goose Up
-- Revalidate current authority immediately before admitting a physical writer
-- or reusing its receipt. A still-live scheduler lease is not authorization.
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;needle text;replacement text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
 'photo.start_v30_archive_complete(uuid,uuid,uuid)',
 'photo.recover_v30_archive_complete(uuid,uuid,uuid)'
 ] LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  needle:=' IF EXISTS(SELECT 1 FROM photo.v30_archive_writers';
  IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'ARCHIVE_WRITER_AUTHORITY_PATCH_MISMATCH: %',signature;END IF;
  replacement:=' IF photo.v2_editor_authorized(p_tenant,d.project_id,d.actor_id,d.completion_session_id) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
'||needle;
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
