-- +goose Up
-- Frozen params bytes must match a completed, immutable version-scoped upload.
-- Proposal/mask scope and task registration are checked separately.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_original_params_scope(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_sha text,p_params_jcs text,
 p_width bigint,p_height bigint,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE params jsonb; wire_bytes bigint;
BEGIN
 IF p_artifact IS NULL OR p_artifact='00000000-0000-0000-0000-000000000000'::uuid
  OR p_params_jcs IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 wire_bytes:=octet_length(p_params_jcs);
 IF wire_bytes NOT BETWEEN 2 AND 1048576
  OR encode(sha256(convert_to(p_params_jcs,'UTF8')),'hex')<>p_sha THEN RETURN false;END IF;
 params:=p_params_jcs::jsonb;
 IF NOT photo.v25_layer_original_preflight(p_tenant,p_project,p_snapshot,p_actor,p_session,
  params,p_width,p_height,p_profile) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads u
  JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id AND a.sha256=u.sha256
  WHERE u.tenant_id=p_tenant AND u.project_id=p_project
   AND u.asset_version_id=(params->'base'->>'asset_version_id')::uuid
   AND u.artifact_id=p_artifact AND u.sha256=p_sha AND u.bytes=wire_bytes
   AND u.state='ready' AND u.completed_at IS NOT NULL
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes=wire_bytes
   AND a.media_type='application/json' AND a.bucket=u.bucket AND a.object_version=u.object_version
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_original_params_scope(uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_original_params_scope(uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_original_params_scope(uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint,bigint,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
