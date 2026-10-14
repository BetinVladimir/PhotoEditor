-- +goose Up
-- Missing nested share limits must not turn the locked upload admission IF
-- into NULL and bypass per-file, aggregate or format constraints.
-- +goose StatementBegin
DO $$
DECLARE definition text;
BEGIN
 SELECT pg_get_functiondef('photo.begin_portal_upload(bytea,bytea,text,text,uuid,uuid,text,text,bigint,text,text,text,text,timestamptz)'::regprocedure)
 INTO definition;
 IF definition IS NULL
  OR position('jsonb_typeof(l.limits)<>''object''' IN definition)=0
  OR position('jsonb_typeof(l.limits->''allowed_formats'')<>''array''' IN definition)=0
  OR position('NOT (l.limits->''allowed_formats'' ? p_format)' IN definition)=0
  OR position('Upload.BeginGuest' IN definition)=0 THEN
  RAISE EXCEPTION 'PORTAL_UPLOAD_LIMITS_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,
  'l.permissions->''upload''<>''true''::jsonb',
  'l.permissions->''upload'' IS DISTINCT FROM ''true''::jsonb');
 definition:=replace(definition,
  'l.policy_version<>sh.link_version',
  'l.policy_version IS DISTINCT FROM sh.link_version');
 definition:=replace(definition,
  'jsonb_typeof(l.limits)<>''object''',
  'jsonb_typeof(l.limits) IS DISTINCT FROM ''object''');
 definition:=replace(definition,
  'jsonb_typeof(l.limits->''max_upload_files'')<>''number''',
  'jsonb_typeof(l.limits->''max_upload_files'') IS DISTINCT FROM ''number''');
 definition:=replace(definition,
  'jsonb_typeof(l.limits->''max_upload_bytes_total'')<>''number''',
  'jsonb_typeof(l.limits->''max_upload_bytes_total'') IS DISTINCT FROM ''number''');
 definition:=replace(definition,
  'jsonb_typeof(l.limits->''max_file_bytes'')<>''number''',
  'jsonb_typeof(l.limits->''max_file_bytes'') IS DISTINCT FROM ''number''');
 definition:=replace(definition,
  'jsonb_typeof(l.limits->''allowed_formats'')<>''array''',
  'jsonb_typeof(l.limits->''allowed_formats'') IS DISTINCT FROM ''array''');
 definition:=replace(definition,
  'NOT (l.limits->''allowed_formats'' ? p_format)',
  'NOT coalesce((l.limits->''allowed_formats'' ? p_format),false)');
 IF position('jsonb_typeof(l.limits)<>''object''' IN definition)>0
  OR position('jsonb_typeof(l.limits->''allowed_formats'')<>''array''' IN definition)>0
  OR position('NOT (l.limits->''allowed_formats'' ? p_format)' IN definition)>0 THEN
  RAISE EXCEPTION 'PORTAL_UPLOAD_LIMITS_REWRITE';
 END IF;
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_portal_upload(bytea,bytea,text,text,uuid,uuid,text,text,bigint,text,text,text,text,timestamptz)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_portal_upload(bytea,bytea,text,text,uuid,uuid,text,text,bigint,text,text,text,text,timestamptz)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_portal_upload(bytea,bytea,text,text,uuid,uuid,text,text,bigint,text,text,text,text,timestamptz)
 TO photo_portal;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
