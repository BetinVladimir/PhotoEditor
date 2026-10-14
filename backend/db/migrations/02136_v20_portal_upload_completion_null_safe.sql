-- +goose Up
-- Multipart completion must never accept NULL parts or malformed entries;
-- both upload mutations must repeat the locked current share permission.
-- +goose StatementBegin
DO $$
DECLARE definition text;signature text;
BEGIN
 signature:='photo.attach_portal_multipart(bytea,bytea,uuid,text)';
 SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
 IF definition IS NULL
  OR position('l.permissions->''upload''<>''true''::jsonb' IN definition)=0
  OR position('l.policy_version<>s.link_version' IN definition)=0 THEN
  RAISE EXCEPTION 'PORTAL_MULTIPART_ATTACH_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,
  'l.permissions->''upload''<>''true''::jsonb',
  'l.permissions->''upload'' IS DISTINCT FROM ''true''::jsonb');
 definition:=replace(definition,
  'l.policy_version<>s.link_version',
  'l.policy_version IS DISTINCT FROM s.link_version');
 EXECUTE definition;

 signature:='photo.complete_portal_upload(bytea,bytea,text,text,uuid,bigint,jsonb,uuid,uuid,uuid)';
 SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
 IF definition IS NULL
  OR position('jsonb_typeof(p_parts)<>''array''' IN definition)=0
  OR position('jsonb_typeof(v->''part_no'')<>''number''' IN definition)=0
  OR position('jsonb_typeof(v->''etag'')<>''string''' IN definition)=0
  OR position('(v->>''part_no'')::numeric<>ord' IN definition)=0
  OR position('l.permissions->''upload''<>''true''::jsonb' IN definition)=0 THEN
  RAISE EXCEPTION 'PORTAL_MULTIPART_COMPLETE_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,
  'p_expected_size NOT BETWEEN 1 AND 536870912',
  'p_expected_size IS NULL OR p_expected_size NOT BETWEEN 1 AND 536870912');
 definition:=replace(definition,
  'jsonb_typeof(p_parts)<>''array''',
  'jsonb_typeof(p_parts) IS DISTINCT FROM ''array''');
 definition:=replace(definition,
  'jsonb_typeof(v)<>''object''',
  'jsonb_typeof(v) IS DISTINCT FROM ''object''');
 definition:=replace(definition,
  'jsonb_typeof(v->''part_no'')<>''number''',
  'jsonb_typeof(v->''part_no'') IS DISTINCT FROM ''number''');
 definition:=replace(definition,
  'jsonb_typeof(v->''etag'')<>''string''',
  'jsonb_typeof(v->''etag'') IS DISTINCT FROM ''string''');
 definition:=replace(definition,
  '(v->>''part_no'')::numeric<>ord',
  '(CASE WHEN jsonb_typeof(v->''part_no'')=''number'' THEN (v->>''part_no'')::numeric ELSE NULL END) IS DISTINCT FROM ord');
 definition:=replace(definition,
  'l.permissions->''upload''<>''true''::jsonb',
  'l.permissions->''upload'' IS DISTINCT FROM ''true''::jsonb');
 definition:=replace(definition,
  'l.policy_version<>s.link_version',
  'l.policy_version IS DISTINCT FROM s.link_version');
 definition:=replace(definition,
  'u.expected_size<>p_expected_size',
  'u.expected_size IS DISTINCT FROM p_expected_size');
 IF position('jsonb_typeof(p_parts)<>''array''' IN definition)>0
  OR position('jsonb_typeof(v->''part_no'')<>''number''' IN definition)>0
  OR position('jsonb_typeof(v->''etag'')<>''string''' IN definition)>0
  OR position('(v->>''part_no'')::numeric<>ord' IN definition)>0 THEN
  RAISE EXCEPTION 'PORTAL_MULTIPART_COMPLETE_REWRITE';
 END IF;
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.attach_portal_multipart(bytea,bytea,uuid,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.complete_portal_upload(bytea,bytea,text,text,uuid,bigint,jsonb,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.attach_portal_multipart(bytea,bytea,uuid,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION photo.complete_portal_upload(bytea,bytea,text,text,uuid,bigint,jsonb,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.attach_portal_multipart(bytea,bytea,uuid,text) TO photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_portal_upload(bytea,bytea,text,text,uuid,bigint,jsonb,uuid,uuid,uuid)
 TO photo_portal;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
