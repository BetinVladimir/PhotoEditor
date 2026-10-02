-- +goose Up
-- Typed registration does not itself grant task/root access or qualify a provider.
ALTER TABLE photo.v2_draft_uploads ADD COLUMN render_source_id uuid,
 ADD COLUMN render_source_sha text,
 ADD COLUMN render_registered_at timestamptz,
 ADD CONSTRAINT v26_render_registration_complete CHECK(
  (render_source_id IS NULL AND render_source_sha IS NULL AND render_registered_at IS NULL)
  OR (render_source_id IS NOT NULL AND render_source_sha IS NOT NULL AND render_source_sha ~ '^[0-9a-f]{64}$' AND render_registered_at IS NOT NULL)),
 ADD CONSTRAINT v26_render_registration_source FOREIGN KEY(tenant_id,render_source_id) REFERENCES photo.artifacts(tenant_id,id);
-- +goose StatementBegin
CREATE FUNCTION photo.register_v26_render_recipe(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_sha text,p_source uuid,p_source_sha text,p_raw text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; body jsonb;
BEGIN
 IF p_tenant IS NULL OR p_version IS NULL OR p_document IS NULL OR p_actor IS NULL OR p_session IS NULL
  OR p_artifact IS NULL OR p_source IS NULL OR p_artifact=p_source
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_raw IS NULL OR octet_length(p_raw) NOT BETWEEN 2 AND 1048576
  OR encode(public.digest(convert_to(p_raw,'UTF8'),'sha256'),'hex')<>p_sha THEN RETURN false;END IF;
 BEGIN body:=p_raw::jsonb; EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR NOT body ?& ARRAY['schema_version','recipe','recipe_sha256','scalars','learning'] OR (body-'schema_version'-'recipe'-'recipe_sha256'-'scalars'-'learning')<>'{}'::jsonb
  OR body->>'schema_version' IS DISTINCT FROM '1' OR jsonb_typeof(body->'recipe') IS DISTINCT FROM 'object'
  OR jsonb_typeof(body->'scalars') IS DISTINCT FROM 'object' OR coalesce(body->>'recipe_sha256','') !~ '^[0-9a-f]{64}$'
  OR body->'recipe'->'source' IS DISTINCT FROM body->'scalars'->'source'
  OR body->'scalars'->'source'->>'asset_version_id' IS DISTINCT FROM p_version::text
  OR body->'scalars'->'source'->'source'->>'artifact_id' IS DISTINCT FROM p_source::text
  OR body->'scalars'->'source'->'source'->>'sha256' IS DISTINCT FROM p_source_sha THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant AND u.document_id=p_document FOR UPDATE;
 IF NOT FOUND OR d.asset_version_id<>p_version OR d.artifact_id<>p_artifact OR d.sha256<>p_sha OR d.state<>'ready'
  OR NOT photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact
  AND a.sha256=p_sha AND a.object_version=d.object_version AND a.bytes=d.bytes AND a.output_role='document'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
  JOIN photo.artifacts src ON src.tenant_id=v.tenant_id AND src.id=v.master_artifact_id AND src.sha256=v.content_hash
  WHERE v.tenant_id=p_tenant AND v.id=p_version AND a.project_id=d.project_id AND a.deleted_at IS NULL
  AND a.upload_state='verified' AND v.review_state='accepted' AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha
  AND src.state='ready' AND src.verified_at IS NOT NULL AND src.output_role='working_image'
  AND src.object_version IS NOT NULL AND src.object_version NOT IN('','null')
  AND (src.expires_at IS NULL OR src.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF d.render_registered_at IS NOT NULL THEN RETURN d.render_source_id=p_source AND d.render_source_sha=p_source_sha;END IF;
 UPDATE photo.v2_draft_uploads SET render_source_id=p_source,render_source_sha=p_source_sha,render_registered_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_v26_render_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v26_render_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v26_render_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
