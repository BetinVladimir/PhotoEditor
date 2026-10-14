-- +goose Up
-- Perspective line evidence is tenant metadata. Retire it only through the
-- existing object-cleanup-gated finish_tenant_deletion transaction.
GRANT DELETE ON photo.v29_perspective_lines TO photo_fn_owner;
-- +goose StatementBegin
DO $$
DECLARE definition text;
        anchor constant text := '''v29_negative_skip_closures'',';
        entry constant text := '''v29_perspective_lines'',';
BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF definition IS NULL OR strpos(definition,anchor)=0 OR strpos(definition,entry)>0
    OR strpos(definition,'cleanup_token=p_token AND cleanup_lease_until>clock_timestamp()')=0
    OR strpos(definition,'v30_archive_writers w WHERE w.tenant_id=j.tenant_id AND w.state=''open''')=0 THEN
  RAISE EXCEPTION 'V29_PERSPECTIVE_LINES_PURGE_SHAPE';
 END IF;
 EXECUTE replace(definition,anchor,entry||anchor);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
