-- +goose Up
ALTER TABLE photo.v2_draft_uploads
 ADD COLUMN render_learning_checked boolean NOT NULL DEFAULT false,
 ADD COLUMN render_learning jsonb,
 ADD COLUMN render_learning_model_lock text,
 ADD CONSTRAINT v26_render_learning_pair CHECK (
  (render_learning IS NULL AND render_learning_model_lock IS NULL)
  OR (render_learning_checked AND jsonb_typeof(render_learning)='object'
   AND render_learning_model_lock IS NOT NULL AND render_learning_model_lock ~ '^[0-9a-f]{64}$'));
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_frozen_learning_ready(p_tenant uuid,p_project uuid,p_version uuid,
 p_root uuid,p_sha text,p_learning jsonb,p_lock text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_learning IS NULL THEN RETURN p_lock IS NULL;END IF;
 IF jsonb_typeof(p_learning) IS DISTINCT FROM 'object'
  OR NOT p_learning ?& ARRAY['version_id','model','descriptor','analysis_id','ordinal']
  OR (p_learning-'version_id'-'model'-'descriptor'-'analysis_id'-'ordinal')<>'{}'::jsonb
  OR jsonb_typeof(p_learning->'ordinal') IS DISTINCT FROM 'number'
  OR coalesce(p_learning->>'ordinal','') !~ '^(0|[1-9][0-9]{0,3})$'
  OR (p_learning->>'ordinal')::integer>4999
  OR coalesce(p_lock,'') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_learning->'model') IS DISTINCT FROM 'object'
  OR jsonb_typeof(p_learning->'descriptor') IS DISTINCT FROM 'object'
  OR NOT p_learning->'model' ?& ARRAY['artifact_id','sha256']
  OR NOT p_learning->'descriptor' ?& ARRAY['artifact_id','sha256']
  OR ((p_learning->'model')-'artifact_id'-'sha256')<>'{}'::jsonb
  OR ((p_learning->'descriptor')-'artifact_id'-'sha256')<>'{}'::jsonb
  OR coalesce(p_learning->'model'->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_learning->'descriptor'->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 RETURN coalesce(photo.v26_render_model_scope(p_tenant,p_project,p_version,p_root,p_sha,
  (p_learning->'model'->>'artifact_id')::uuid,p_learning->'model'->>'sha256',
  (p_learning->>'version_id')::uuid,p_actor,p_session)
 AND photo.v26_render_descriptor_scope(p_tenant,p_project,p_version,p_root,p_sha,
  (p_learning->'descriptor'->>'artifact_id')::uuid,p_learning->'descriptor'->>'sha256',
  (p_learning->>'analysis_id')::uuid,(p_learning->>'ordinal')::integer,p_lock,p_actor,p_session),false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_frozen_learning_ready(uuid,uuid,uuid,uuid,text,jsonb,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_frozen_learning_ready(uuid,uuid,uuid,uuid,text,jsonb,text,uuid,uuid) FROM PUBLIC;
ALTER FUNCTION photo.register_v26_render_draft_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint)
 RENAME TO register_v26_render_draft_recipe_core;
REVOKE ALL ON FUNCTION photo.register_v26_render_draft_recipe_core(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) FROM PUBLIC,photo_api;
-- +goose StatementBegin
CREATE FUNCTION photo.register_v26_render_draft_recipe_result(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_sha text,p_source uuid,p_source_sha text,p_raw text,
 p_draft_document uuid,p_expected bigint)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb; learning jsonb; model_lock text; project uuid;
BEGIN
 IF p_raw IS NULL OR octet_length(p_raw) NOT BETWEEN 2 AND 1048576 THEN RETURN 'conflict';END IF;
 BEGIN body:=p_raw::jsonb;EXCEPTION WHEN invalid_text_representation THEN RETURN 'conflict';END;
 learning:=nullif(body->'learning','null'::jsonb);
 IF learning IS NOT NULL THEN
  IF body->'recipe'->>'learning_version_id' IS DISTINCT FROM learning->>'version_id' THEN RETURN 'conflict';END IF;
  model_lock:=body->'scalars'->>'model_lock_sha';
 ELSIF body->'recipe'->>'learning_version_id' IS NOT NULL THEN RETURN 'conflict';END IF;
 -- This subtransaction rolls back core registration if live learning scope fails,
 -- including direct SQL callers that commit a false return value.
 BEGIN
  IF NOT photo.register_v26_render_draft_recipe_core(p_tenant,p_version,p_document,p_actor,p_session,
   p_artifact,p_sha,p_source,p_source_sha,p_raw,p_draft_document,p_expected) THEN
   RAISE EXCEPTION 'freeze registration denied' USING ERRCODE='PF001';END IF;
  SELECT project_id INTO project FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND document_id=p_document;
  IF NOT photo.v26_render_frozen_learning_ready(p_tenant,project,p_version,p_artifact,p_sha,learning,model_lock,p_actor,p_session) THEN
   RAISE EXCEPTION 'freeze learning denied' USING ERRCODE='PF002';END IF;
  UPDATE photo.v2_draft_uploads SET render_learning_checked=true,render_learning=learning,render_learning_model_lock=model_lock
   WHERE tenant_id=p_tenant AND document_id=p_document;
  RETURN 'registered';
 EXCEPTION WHEN SQLSTATE 'PF001' THEN RETURN 'conflict'; WHEN SQLSTATE 'PF002' THEN RETURN 'learning_expired';END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_v26_render_draft_recipe_result(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v26_render_draft_recipe_result(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v26_render_draft_recipe_result(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) TO photo_api;
-- +goose StatementBegin
CREATE FUNCTION photo.register_v26_render_draft_recipe(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_sha text,p_source uuid,p_source_sha text,p_raw text,
 p_draft_document uuid,p_expected bigint)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.register_v26_render_draft_recipe_result(p_tenant,p_version,p_document,p_actor,p_session,
 p_artifact,p_sha,p_source,p_source_sha,p_raw,p_draft_document,p_expected)='registered';
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_v26_render_draft_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v26_render_draft_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v26_render_draft_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint) TO photo_api;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_render_frozen_draft_scope(p_tenant uuid,p_asset uuid,p_document uuid,
 p_draft uuid,p_revision bigint,p_artifact uuid,p_sha text,p_version uuid,p_source uuid,p_source_sha text,
 p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid,false)
 AND p_revision>0 AND EXISTS(
  SELECT 1 FROM photo.v2_draft_uploads u
  JOIN photo.asset_versions v ON v.tenant_id=u.tenant_id AND v.id=u.asset_version_id
  JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.project_id=u.project_id
  WHERE u.tenant_id=p_tenant AND a.id=p_asset AND a.deleted_at IS NULL
   AND v.id=p_version AND u.document_id=p_document AND u.artifact_id=p_artifact AND u.sha256=p_sha
   AND u.render_source_id=p_source AND u.render_source_sha=p_source_sha
   AND u.render_draft_document_id=p_draft AND u.render_draft_revision=p_revision
   AND u.render_learning_checked
   AND photo.v26_render_frozen_learning_ready(p_tenant,u.project_id,v.id,p_artifact,p_sha,u.render_learning,u.render_learning_model_lock,p_actor,p_session)
   AND photo.v26_render_recipe_scope(p_tenant,u.project_id,v.id,p_artifact,p_sha,p_actor,p_session)
 )
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_frozen_draft_scope(uuid,uuid,uuid,uuid,bigint,uuid,text,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_frozen_draft_scope(uuid,uuid,uuid,uuid,bigint,uuid,text,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_frozen_draft_scope(uuid,uuid,uuid,uuid,bigint,uuid,text,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
