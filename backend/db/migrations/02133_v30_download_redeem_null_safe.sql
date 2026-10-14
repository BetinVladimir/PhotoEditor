-- +goose Up
-- A missing JSON permission evaluates NULL with <> and must never authorize
-- item/volume redemption. Preserve the exact Delivery branch from 01881.
-- +goose StatementBegin
DO $$
DECLARE definition text;
BEGIN
 SELECT pg_get_functiondef('photo.redeem_download_intent(bytea,text)'::regprocedure)
 INTO definition;
 IF definition IS NULL
  OR position('i.intent_kind=''delivery''' IN definition)=0
  OR position('l.permissions->''download''<>''true''::jsonb' IN definition)=0
  OR position('jsonb_typeof(l.allowed_presets)<>''array''' IN definition)=0
  OR position('jsonb_array_elements(l.allowed_presets)' IN definition)=0
  OR position('v30_delivery_download_target' IN definition)=0 THEN
  RAISE EXCEPTION 'DOWNLOAD_REDEEM_NULL_SAFE_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,
  'l.permissions->''download''<>''true''::jsonb',
  'l.permissions->''download'' IS DISTINCT FROM ''true''::jsonb');
 definition:=replace(definition,
  'jsonb_typeof(l.allowed_presets)<>''array''',
  'jsonb_typeof(l.allowed_presets) IS DISTINCT FROM ''array''');
 definition:=replace(definition,
  'jsonb_array_elements(l.allowed_presets)',
  'jsonb_array_elements(CASE WHEN jsonb_typeof(l.allowed_presets)=''array'' THEN l.allowed_presets ELSE ''[]''::jsonb END)');
 definition:=replace(definition,
  's.link_version<>i.policy_version',
  's.link_version IS DISTINCT FROM i.policy_version');
 definition:=replace(definition,
  'l.policy_version<>i.policy_version',
  'l.policy_version IS DISTINCT FROM i.policy_version');
 definition:=replace(definition,
  'j.share_id<>i.share_id',
  'j.share_id IS DISTINCT FROM i.share_id');
 definition:=replace(definition,
  'j.policy_version<>i.policy_version',
  'j.policy_version IS DISTINCT FROM i.policy_version');
 IF position('l.permissions->''download''<>''true''::jsonb' IN definition)>0
  OR position('jsonb_typeof(l.allowed_presets)<>''array''' IN definition)>0
  OR position('jsonb_array_elements(l.allowed_presets)' IN definition)>0 THEN
  RAISE EXCEPTION 'DOWNLOAD_REDEEM_NULL_SAFE_REWRITE';
 END IF;
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.redeem_download_intent(bytea,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.redeem_download_intent(bytea,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.redeem_download_intent(bytea,text) TO photo_api,photo_portal;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
