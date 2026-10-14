-- Reauthorize inline background pins before freezing the learning snapshot.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_registered_imported_background(p_tenant uuid,p_project uuid,p_version uuid,p_artifact uuid,p_sha text,p_portable text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.styles s JOIN photo.style_versions v ON v.tenant_id=s.tenant_id AND v.style_id=s.id
 JOIN photo.style_asset_bindings b ON b.tenant_id=v.tenant_id AND b.style_version_id=v.id
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id AND a.sha256=b.sha256
 JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id AND d.owner_id=a.id
 WHERE s.tenant_id=p_tenant AND s.project_id=p_project AND s.archived_at IS NULL
 AND v.id=p_version AND v.approval_status='accepted' AND v.metrics->>'schema_version'='interchange-style-approval/v1'
 AND b.portable_asset_id=p_portable AND b.artifact_id=p_artifact AND b.sha256=p_sha
 AND p_portable=p_artifact::text AND a.output_role='v30_import_prepared'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.bytes BETWEEN 8 AND 536870912 AND a.media_type IN('image/jpeg','image/png','image/tiff')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND d.purpose='v30_import_prepared' AND d.state='ready' AND d.sha256=a.sha256 AND d.bytes=a.bytes
 AND d.bucket=a.bucket AND d.object_version=a.object_version
 AND d.v30_import_prepared->>'contract'='StyleBackgroundAsset'
 AND d.v30_import_prepared->>'workflow'=v.metrics->>'workflow_id'
 AND d.v30_import_prepared->>'entry'=v.metrics->>'entry_id'
 AND d.v30_import_prepared#>'{background_occurrence,style}'=jsonb_build_object(
 'artifact_id',v.metrics#>>'{original,artifact_id}','sha256',v.metrics#>>'{original,sha256}')
 AND v.config_json#>>'{extensions,background,mode}'='asset'
 AND v.config_json#>>'{extensions,background,asset_id}'=p_portable);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_registered_imported_background(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_registered_imported_background(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
ALTER FUNCTION photo.freeze_v23_training_snapshot(uuid,uuid,uuid,uuid,uuid,bigint,jsonb,text,text)
 RENAME TO freeze_v23_training_snapshot_pre_background_pin;
REVOKE ALL ON FUNCTION photo.freeze_v23_training_snapshot_pre_background_pin(uuid,uuid,uuid,uuid,uuid,bigint,jsonb,text,text)
 FROM PUBLIC,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.freeze_v23_training_snapshot(p_tenant uuid,p_run uuid,
 p_profile uuid,p_actor uuid,p_auth_session uuid,p_expected bigint,
 p_manifest jsonb,p_manifest_sha text,p_snapshot_body_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project_id uuid; example jsonb; pin jsonb; matches boolean;
BEGIN
 -- Preserve tenant/profile/run lock order before style/asset shared locks.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT p.project_id INTO project_id FROM photo.learning_profiles p
  WHERE p.tenant_id=p_tenant AND p.id=p_profile FOR UPDATE;
 IF project_id IS NULL OR NOT photo.v2_editor_authorized(p_tenant,project_id,p_actor,p_auth_session)
  OR jsonb_typeof(p_manifest->'examples') IS DISTINCT FROM 'array'
  OR jsonb_array_length(p_manifest->'examples') NOT BETWEEN 40 AND 5000
  THEN RETURN false; END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 FOR example IN SELECT value FROM jsonb_array_elements(p_manifest->'examples') LOOP
  IF NOT (example->'render_case' ? 'background_asset') THEN CONTINUE; END IF;
  pin:=example->'render_case'->'background_asset';
  IF jsonb_typeof(pin) IS DISTINCT FROM 'object' OR
   (SELECT count(*) FROM jsonb_object_keys(pin))<>10 OR
   pin->>'schema_version' IS DISTINCT FROM '1' OR
   jsonb_typeof(pin->'schema_version') IS DISTINCT FROM 'number' OR
   pin->>'work_icc_sha' IS DISTINCT FROM p_manifest->>'icc_lock_sha' OR
   coalesce(pin->>'work_icc_sha','') !~ '^[0-9a-f]{64}$' OR
   jsonb_typeof(pin->'assume_profile') IS DISTINCT FROM 'null' OR
   coalesce(pin->>'portable_asset_id','')='' OR
   octet_length(pin->>'portable_asset_id')>120 OR
   btrim(pin->>'portable_asset_id')<>pin->>'portable_asset_id' OR
   jsonb_typeof(pin->'expected_bytes') IS DISTINCT FROM 'number' OR
   coalesce(pin->>'expected_bytes','') !~ '^[1-9][0-9]{0,8}$' OR
   (pin->>'expected_bytes')::bigint>536870912 OR
   pin->>'suffix' IS DISTINCT FROM CASE pin->>'media_type'
    WHEN 'image/jpeg' THEN '.jpg' WHEN 'image/png' THEN '.png' WHEN 'image/tiff' THEN '.tiff' ELSE '' END
   THEN RETURN false; END IF;
  -- Compare untrusted IDs as text: malformed UUIDs cannot throw during casts.
  -- The style JSON independently declares the same portable background.
  PERFORM a.id FROM photo.styles s
   JOIN photo.style_versions v ON v.tenant_id=s.tenant_id AND v.style_id=s.id
   JOIN photo.style_asset_bindings b ON b.tenant_id=v.tenant_id AND b.style_version_id=v.id
   JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id AND a.sha256=b.sha256
   JOIN photo.artifacts config ON config.tenant_id=v.tenant_id AND config.id=v.export_artifact_id AND config.sha256=v.config_hash
   WHERE s.tenant_id=p_tenant AND s.project_id=project_id AND s.archived_at IS NULL
    AND v.id::text=pin->>'style_version_id' AND v.approval_status='accepted'
    AND v.export_artifact_id::text=pin->'style'->>'artifact_id'
    AND v.config_hash=pin->'style'->>'sha256'
    AND v.config_json#>>'{extensions,background,mode}'='asset'
    AND v.config_json#>>'{extensions,background,asset_id}'=pin->>'portable_asset_id'
    AND b.portable_asset_id=pin->>'portable_asset_id'
    AND a.id::text=pin->'source'->>'artifact_id' AND a.sha256=pin->'source'->>'sha256'
    AND a.bytes=(pin->>'expected_bytes')::bigint AND a.media_type=pin->>'media_type'
    AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
    AND a.object_version NOT IN ('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND config.state='ready' AND config.verified_at IS NOT NULL AND config.object_version IS NOT NULL
    AND config.object_version NOT IN ('','null') AND (config.expires_at IS NULL OR config.expires_at>clock_timestamp())
    AND (a.output_role='style_asset' OR (a.output_role='v30_import_prepared'
     AND photo.v23_registered_imported_background(p_tenant,project_id,v.id,a.id,a.sha256,b.portable_asset_id) IS TRUE))
   FOR SHARE OF s,v,b,a,config;
  matches:=FOUND;
  IF NOT matches THEN RETURN false; END IF;
 END LOOP;
 RETURN photo.freeze_v23_training_snapshot_pre_background_pin(p_tenant,p_run,p_profile,p_actor,p_auth_session,p_expected,p_manifest,p_manifest_sha,p_snapshot_body_sha);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.freeze_v23_training_snapshot(uuid,uuid,uuid,uuid,uuid,bigint,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.freeze_v23_training_snapshot(uuid,uuid,uuid,uuid,uuid,bigint,jsonb,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.freeze_v23_training_snapshot(uuid,uuid,uuid,uuid,uuid,bigint,jsonb,text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
