-- +goose Up
-- The locked second admission check must reject a removed permission even if
-- authorize_portal_session saw the previous share policy.
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz)',
  'photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid)'
 ] LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  IF definition IS NULL
   OR position('l.permissions->''download''<>''true''::jsonb' IN definition)=0
   OR position('l.policy_version<>s.link_version' IN definition)=0
   OR position('authorize_portal_session' IN definition)=0 THEN
   RAISE EXCEPTION 'PORTAL_DOWNLOAD_NULL_SAFE_SOURCE_SHAPE: %',signature;
  END IF;
  definition:=replace(definition,
   'l.permissions->''download''<>''true''::jsonb',
   'l.permissions->''download'' IS DISTINCT FROM ''true''::jsonb');
  definition:=replace(definition,
   'l.permissions->''view''<>''true''::jsonb',
   'l.permissions->''view'' IS DISTINCT FROM ''true''::jsonb');
  definition:=replace(definition,
   'l.policy_version<>s.link_version',
   'l.policy_version IS DISTINCT FROM s.link_version');
  IF position('l.permissions->''download''<>''true''::jsonb' IN definition)>0
   OR position('l.permissions->''view''<>''true''::jsonb' IN definition)>0 THEN
   RAISE EXCEPTION 'PORTAL_DOWNLOAD_NULL_SAFE_REWRITE: %',signature;
  END IF;
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz)
 OWNER TO photo_fn_owner;
ALTER FUNCTION photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz)
 FROM PUBLIC;
REVOKE ALL ON FUNCTION photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz)
 TO photo_portal;
GRANT EXECUTE ON FUNCTION photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid)
 TO photo_portal;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
