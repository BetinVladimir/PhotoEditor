-- +goose Up
-- Commit-time authority proof; caller must first lock tenant -> run -> task.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_locked_scope(p_tenant uuid,p_actor uuid,p_session uuid,p_params jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid; asset uuid; version uuid; source_id uuid; recipe_id uuid; s record; f record;
BEGIN
 IF p_params->>'schema_version' IS DISTINCT FROM '1' OR p_params->>'tenant_id' IS DISTINCT FROM p_tenant::text
  OR p_params->'frozen_recipe'->'source'->'source' IS DISTINCT FROM p_params->'source'->'ref'
  OR p_params->'request'->'recipe' IS DISTINCT FROM p_params->'recipe'->'ref'
  OR p_params->'frozen_recipe'->'source'->>'asset_version_id' IS DISTINCT FROM p_params->'request'->>'version_id'
  THEN RETURN false; END IF;
 project:=(p_params->>'project_id')::uuid; asset:=(p_params->>'asset_id')::uuid;
 version:=(p_params->'request'->>'version_id')::uuid;
 source_id:=(p_params->'source'->'ref'->>'artifact_id')::uuid;
 recipe_id:=(p_params->'recipe'->'ref'->>'artifact_id')::uuid;
 PERFORM id FROM photo.users WHERE id=p_actor FOR SHARE;
 PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor FOR SHARE;
 PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project FOR SHARE;
 PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=project AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.assets WHERE tenant_id=p_tenant AND id=asset FOR SHARE;
 PERFORM id FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=version FOR SHARE;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(source_id,recipe_id) ORDER BY a.id FOR SHARE;
 PERFORM d.id FROM photo.backend_documents d
 JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE d.tenant_id=p_tenant AND d.artifact_id=recipe_id FOR SHARE OF d,r;
 SELECT * INTO s FROM photo.v26_region_source(p_tenant,asset,version,p_actor,p_session);
 SELECT * INTO f FROM photo.v26_region_recipe(p_tenant,asset,version,p_actor,p_session,recipe_id,p_params->'recipe'->'ref'->>'sha256');
 IF s.artifact_id IS NULL OR f.artifact_id IS NULL OR s.project_id IS DISTINCT FROM project OR f.project_id IS DISTINCT FROM project
  OR s.artifact_id IS DISTINCT FROM source_id OR s.sha256 IS DISTINCT FROM p_params->'source'->'ref'->>'sha256'
  OR s.bucket IS DISTINCT FROM p_params->'source'->>'bucket' OR s.object_key IS DISTINCT FROM p_params->'source'->>'object_key'
  OR s.object_version IS DISTINCT FROM p_params->'source'->>'object_version' OR s.bytes IS DISTINCT FROM (p_params->'source'->>'bytes')::bigint
  OR f.bucket IS DISTINCT FROM p_params->'recipe'->>'bucket' OR f.object_key IS DISTINCT FROM p_params->'recipe'->>'object_key'
  OR f.object_version IS DISTINCT FROM p_params->'recipe'->>'object_version' OR f.bytes IS DISTINCT FROM (p_params->'recipe'->>'bytes')::bigint
  THEN RETURN false; END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_region_locked_scope(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_locked_scope(uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_locked_scope(uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
