-- +goose Up
-- Original accepted source only. This predicate is not worker admission:
-- lease/Params/qualification and target/protected producer proofs are separate.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_portrait_source_scope(p_tenant uuid,p_project uuid,
 p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_ref jsonb;
BEGIN
 IF p_base IS NULL OR jsonb_typeof(p_base) IS DISTINCT FROM 'object'
  OR NOT (p_base ?& ARRAY['asset_version_id','source','canvas_hash'])
  OR p_base-ARRAY['asset_version_id','source','canvas_hash']<>'{}'::jsonb
  OR jsonb_typeof(p_base->'asset_version_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(p_base->'canvas_hash') IS DISTINCT FROM 'string'
  OR (p_base->>'canvas_hash') !~ '^[0-9a-f]{64}$'
  OR photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 source_ref:=p_base->'source';
 IF jsonb_typeof(source_ref) IS DISTINCT FROM 'object'
  OR NOT (source_ref ?& ARRAY['artifact_id','sha256'])
  OR source_ref-ARRAY['artifact_id','sha256']<>'{}'::jsonb
  OR jsonb_typeof(source_ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(source_ref->'sha256') IS DISTINCT FROM 'string'
  OR (source_ref->>'sha256') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.snapshot_items i
  JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
  JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
  JOIN photo.artifacts s ON s.tenant_id=i.tenant_id AND s.id=i.artifact_id AND s.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot
   AND i.asset_version_id=(p_base->>'asset_version_id')::uuid
   AND i.artifact_id=(source_ref->>'artifact_id')::uuid AND i.sha256=source_ref->>'sha256'
   AND v.review_state='accepted' AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
   AND a.project_id=p_project AND a.deleted_at IS NULL AND a.upload_state='verified'
   AND s.output_role='working_image' AND s.media_type='application/json'
   AND s.state='ready' AND s.verified_at IS NOT NULL
   AND s.object_version IS NOT NULL AND s.object_version NOT IN('','null')
   AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_local_portrait_source_scope(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_source_scope(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_local_portrait_source_scope(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
