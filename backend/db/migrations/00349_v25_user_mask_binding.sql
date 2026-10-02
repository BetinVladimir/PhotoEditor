-- +goose Up
-- Persist validated new-user MaskRef dependencies in the existing upload row.
ALTER TABLE photo.v2_draft_uploads ADD COLUMN user_mask_binding jsonb
 CHECK(user_mask_binding IS NULL OR (upload_contract='Document' AND jsonb_typeof(user_mask_binding)='object' AND pg_column_size(user_mask_binding)<=1048576));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v25_user_mask_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.user_mask_binding IS NOT NULL AND NEW.user_mask_binding IS DISTINCT FROM OLD.user_mask_binding THEN
  RAISE EXCEPTION 'immutable user_mask_binding' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER v25_user_mask_binding_immutable BEFORE UPDATE ON photo.v2_draft_uploads
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v25_user_mask_binding();
-- +goose StatementBegin
CREATE FUNCTION photo.bind_v25_user_mask(p_tenant uuid,p_version uuid,p_actor uuid,p_session uuid,
 p_root uuid,p_raw text,p_source uuid,p_source_sha text,p_canvas text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; m jsonb; binding jsonb; w bigint; h bigint; pixels record; digest text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_raw IS NULL OR octet_length(p_raw) NOT BETWEEN 2 AND 1048576
  OR p_source IS NULL OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_canvas,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 BEGIN m:=p_raw::jsonb;EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF jsonb_typeof(m) IS DISTINCT FROM 'object' OR NOT m ?& ARRAY['artifact','canvas','transform_chain','object_id','selector','confidence','model_lock_id','protected'] THEN RETURN false;END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(m))<>8
  OR jsonb_typeof(m->'artifact') IS DISTINCT FROM 'object'
  OR coalesce(m->'artifact'->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(m->'artifact'->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR coalesce(m->'canvas'->>'width','') !~ '^[1-9][0-9]{0,8}$'
  OR coalesce(m->'canvas'->>'height','') !~ '^[1-9][0-9]{0,8}$' THEN RETURN false;END IF;
 w:=(m->'canvas'->>'width')::bigint;h:=(m->'canvas'->>'height')::bigint;
 IF w>200000000 OR h>200000000/w THEN RETURN false;END IF;
 digest:=encode(sha256(convert_to(p_raw,'UTF8')),'hex');
 SELECT * INTO d FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant AND u.artifact_id=p_root FOR UPDATE;
 IF NOT FOUND OR d.upload_contract<>'Document' OR d.asset_version_id IS DISTINCT FROM p_version
  OR d.sha256<>digest OR d.bytes<>octet_length(p_raw) OR d.state<>'ready' OR d.completed_at IS NULL
  OR NOT photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_root
  AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
  AND a.media_type='application/json' AND a.output_role='document' AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
  WHERE v.tenant_id=p_tenant AND v.id=p_version AND a.project_id=d.project_id
  AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha AND v.review_state='accepted'
  AND a.deleted_at IS NULL AND a.upload_state='verified') THEN RETURN false;END IF;
 SELECT * INTO pixels FROM photo.read_v25_user_mask_pixels(p_tenant,p_version,p_actor,p_session,
  (m->'artifact'->>'artifact_id')::uuid,m->'artifact'->>'sha256',w,h);
 IF NOT FOUND OR pixels.bucket<>d.bucket THEN RETURN false;END IF;
 binding:=jsonb_build_object('source',jsonb_build_object('artifact_id',p_source::text,'sha256',p_source_sha),'canvas_hash',p_canvas,'mask',m);
 IF d.user_mask_binding IS NOT NULL THEN RETURN d.user_mask_binding=binding;END IF;
 UPDATE photo.v2_draft_uploads SET user_mask_binding=binding WHERE tenant_id=p_tenant AND document_id=d.document_id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.bind_v25_user_mask(uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_v25_user_mask(uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.bind_v25_user_mask(uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
