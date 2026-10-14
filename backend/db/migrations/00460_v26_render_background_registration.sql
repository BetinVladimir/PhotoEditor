-- +goose Up
ALTER TABLE photo.v2_draft_uploads ADD COLUMN render_background_asset jsonb
 CHECK(render_background_asset IS NULL OR jsonb_typeof(render_background_asset)='object');
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_background_binding_ready(p_tenant uuid,p_project uuid,p_pin jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF jsonb_typeof(p_pin) IS DISTINCT FROM 'object'
  OR NOT p_pin ?& ARRAY['schema_version','portable_asset_id','style_version_id','style','source','expected_bytes','media_type','suffix','assume_profile','work_icc_sha']
  OR (p_pin-'schema_version'-'portable_asset_id'-'style_version_id'-'style'-'source'-'expected_bytes'-'media_type'-'suffix'-'assume_profile'-'work_icc_sha')<>'{}'::jsonb
  OR p_pin->>'schema_version' IS DISTINCT FROM '1'
  OR jsonb_typeof(p_pin->'schema_version') IS DISTINCT FROM 'number'
  OR jsonb_typeof(p_pin->'portable_asset_id') IS DISTINCT FROM 'string'
  OR octet_length(p_pin->>'portable_asset_id') NOT BETWEEN 1 AND 120
  OR btrim(p_pin->>'portable_asset_id')<>p_pin->>'portable_asset_id'
  OR coalesce(p_pin->>'work_icc_sha','') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_pin->'expected_bytes') IS DISTINCT FROM 'number'
  OR coalesce(p_pin->>'expected_bytes','') !~ '^[1-9][0-9]{0,8}$'
  OR (p_pin->>'expected_bytes')::bigint>536870912
  OR (p_pin->'assume_profile'<>'null'::jsonb AND p_pin->>'assume_profile' NOT IN ('srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'))
  OR p_pin->>'suffix' IS DISTINCT FROM (CASE p_pin->>'media_type' WHEN 'image/jpeg' THEN '.jpg' WHEN 'image/png' THEN '.png' WHEN 'image/tiff' THEN '.tiff' END)
  OR p_pin->>'media_type' NOT IN ('image/jpeg','image/png','image/tiff') THEN RETURN false;END IF;
 IF jsonb_typeof(p_pin->'style') IS DISTINCT FROM 'object' OR jsonb_typeof(p_pin->'source') IS DISTINCT FROM 'object'
  OR NOT p_pin->'style' ?& ARRAY['artifact_id','sha256'] OR NOT p_pin->'source' ?& ARRAY['artifact_id','sha256']
  OR ((p_pin->'style')-'artifact_id'-'sha256')<>'{}'::jsonb OR ((p_pin->'source')-'artifact_id'-'sha256')<>'{}'::jsonb
  OR coalesce(p_pin->'style'->>'sha256','') !~ '^[0-9a-f]{64}$' OR coalesce(p_pin->'source'->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM 1 FROM photo.styles s JOIN photo.style_versions v ON v.tenant_id=s.tenant_id AND v.style_id=s.id
 JOIN photo.style_asset_bindings b ON b.tenant_id=v.tenant_id AND b.style_version_id=v.id
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id AND a.sha256=b.sha256
 JOIN photo.artifacts config ON config.tenant_id=v.tenant_id AND config.id=v.export_artifact_id AND config.sha256=v.config_hash
 WHERE s.tenant_id=p_tenant AND s.project_id=p_project AND s.archived_at IS NULL
  AND v.id=(p_pin->>'style_version_id')::uuid AND v.approval_status='accepted'
  AND v.export_artifact_id=(p_pin->'style'->>'artifact_id')::uuid AND v.config_hash=p_pin->'style'->>'sha256'
  AND v.config_json#>>'{extensions,background,mode}'='asset'
  AND v.config_json#>>'{extensions,background,asset_id}'=p_pin->>'portable_asset_id'
  AND b.portable_asset_id=p_pin->>'portable_asset_id'
  AND a.id=(p_pin->'source'->>'artifact_id')::uuid AND a.sha256=p_pin->'source'->>'sha256'
  AND a.bytes=(p_pin->>'expected_bytes')::bigint AND a.media_type=p_pin->>'media_type'
  AND a.output_role='style_asset' AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND config.state='ready' AND config.verified_at IS NOT NULL
  AND config.object_version IS NOT NULL AND config.object_version NOT IN ('','null')
  AND (config.expires_at IS NULL OR config.expires_at>clock_timestamp())
 FOR SHARE OF s,v,b,a,config;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_background_binding_ready(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_background_binding_ready(uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.register_v26_render_recipe_source(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_sha text,p_source uuid,p_source_sha text,p_raw text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; body jsonb; background jsonb;
BEGIN
 IF p_tenant IS NULL OR p_version IS NULL OR p_document IS NULL OR p_actor IS NULL OR p_session IS NULL
  OR p_artifact IS NULL OR p_source IS NULL OR p_artifact=p_source
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_raw IS NULL OR octet_length(p_raw) NOT BETWEEN 2 AND 1048576
  OR encode(public.digest(convert_to(p_raw,'UTF8'),'sha256'),'hex')<>p_sha THEN RETURN false;END IF;
 BEGIN body:=p_raw::jsonb; EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR NOT body ?& ARRAY['schema_version','recipe','recipe_sha256','scalars','learning'] OR (body-'schema_version'-'recipe'-'recipe_sha256'-'scalars'-'learning'-'background_asset')<>'{}'::jsonb
  OR body->>'schema_version' IS DISTINCT FROM '1' OR jsonb_typeof(body->'recipe') IS DISTINCT FROM 'object'
  OR jsonb_typeof(body->'scalars') IS DISTINCT FROM 'object' OR coalesce(body->>'recipe_sha256','') !~ '^[0-9a-f]{64}$'
  OR body->'recipe'->'source' IS DISTINCT FROM body->'scalars'->'source'
  OR body->'scalars'->'source'->>'asset_version_id' IS DISTINCT FROM p_version::text
  OR body->'scalars'->'source'->'source'->>'artifact_id' IS DISTINCT FROM p_source::text
  OR body->'scalars'->'source'->'source'->>'sha256' IS DISTINCT FROM p_source_sha THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant AND u.document_id=p_document FOR UPDATE;
 IF NOT FOUND OR d.asset_version_id<>p_version OR d.artifact_id<>p_artifact OR d.sha256<>p_sha OR d.state<>'ready'
  OR NOT photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) THEN RETURN false;END IF;
 background:=body->'background_asset';
 IF body ? 'background_asset' AND (background->'style' IS DISTINCT FROM body->'recipe'->'base_style'
  OR background->'style' IS DISTINCT FROM body->'scalars'->'base_style'
  OR background->>'work_icc_sha' IS DISTINCT FROM body->'scalars'->>'icc_lock_sha'
  OR NOT photo.v26_render_background_binding_ready(p_tenant,d.project_id,background)) THEN RETURN false;END IF;
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
 IF d.render_registered_at IS NOT NULL THEN RETURN d.render_source_id=p_source AND d.render_source_sha=p_source_sha AND d.render_background_asset IS NOT DISTINCT FROM background;END IF;
 UPDATE photo.v2_draft_uploads SET render_source_id=p_source,render_source_sha=p_source_sha,render_registered_at=clock_timestamp(),render_background_asset=background
  WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_v26_render_recipe_source(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v26_render_recipe_source(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
