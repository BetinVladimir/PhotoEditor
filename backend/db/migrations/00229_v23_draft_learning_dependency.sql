-- Draft publication with a learned prediction must pin an active, eligible
-- version and its live consent epochs in the same tenant-locked transaction.
-- The V21 function remains private for its implementation, but API callers
-- must use this V23 boundary even when no learning version is requested.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.replace_v23_draft(
 p_tenant uuid,p_asset_version uuid,p_expected bigint,p_document uuid,
 p_artifact uuid,p_sha text,p_actor uuid,p_session uuid,p_refs jsonb,
 p_base_document uuid,p_base_artifact uuid,p_base_sha text,p_base_refs jsonb,
 p_layer_document uuid,p_layer_artifact uuid,p_layer_sha text,
 p_layer_refs jsonb,p_learning_version uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb; document_project uuid;
BEGIN
 result:=photo.replace_v2_draft(p_tenant,p_asset_version,p_expected,
  p_document,p_artifact,p_sha,p_actor,p_session,p_refs,p_base_document,
  p_base_artifact,p_base_sha,p_base_refs,p_layer_document,p_layer_artifact,
  p_layer_sha,p_layer_refs);
 IF p_learning_version IS NULL THEN RETURN result; END IF;
 SELECT project_id INTO document_project FROM photo.v2_documents
  WHERE tenant_id=p_tenant AND id=p_document;
 IF document_project IS NULL OR NOT EXISTS(
  SELECT 1 FROM photo.learning_versions version
  JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
   AND profile.project_id=version.project_id AND profile.id=version.profile_id
  JOIN photo.version_eligibility state ON state.tenant_id=version.tenant_id
   AND state.project_id=version.project_id AND state.version_id=version.id
  WHERE version.tenant_id=p_tenant AND version.project_id=document_project
   AND version.id=p_learning_version AND state.status='eligible'
   AND profile.active_version_id=version.id
   AND photo.v23_learning_version_consents_live(p_tenant,version.id)) THEN
  RAISE EXCEPTION 'V23_PROFILE_CONSENT_REVOKED' USING ERRCODE='40001';
 END IF;
 INSERT INTO photo.model_use_links(tenant_id,project_id,document_id,version_id)
 VALUES(p_tenant,document_project,p_document,p_learning_version);
 RETURN result;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.replace_v23_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,
 uuid,uuid,text,jsonb,uuid,uuid,text,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.replace_v23_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,
 uuid,uuid,text,jsonb,uuid,uuid,text,jsonb,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.replace_v23_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,
 uuid,uuid,text,jsonb,uuid,uuid,text,jsonb,uuid) TO photo_api;
REVOKE EXECUTE ON FUNCTION photo.replace_v2_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,
 uuid,uuid,text,jsonb,uuid,uuid,text,jsonb) FROM photo_api;
REVOKE EXECUTE ON FUNCTION photo.replace_v2_draft(uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,jsonb,
 uuid,uuid,text,jsonb) FROM photo_api;
