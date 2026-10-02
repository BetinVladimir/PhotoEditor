-- A profile can switch active versions while an approved draft remains
-- eligible. Approval and replay require the version pinned by that draft to
-- remain the explicitly active version under the tenant lifecycle lock.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.approve_v23_draft(p_tenant uuid,p_asset_version uuid,
 p_expected bigint,p_document uuid,p_actor uuid,p_session uuid,
 p_resolved_artifact uuid,p_resolved_sha text,p_learning_version uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb; document_project uuid;
BEGIN
 result:=photo.approve_v2_draft(p_tenant,p_asset_version,p_expected,
  p_document,p_actor,p_session,p_resolved_artifact,p_resolved_sha);
 SELECT project_id INTO document_project FROM photo.v2_documents
  WHERE tenant_id=p_tenant AND id=p_document;
 IF document_project IS NULL THEN
  RAISE EXCEPTION 'V23_APPROVAL_DOCUMENT_MISSING' USING ERRCODE='40001';
 END IF;
 IF p_learning_version IS NULL THEN
  IF EXISTS(SELECT 1 FROM photo.model_use_links link
   WHERE link.tenant_id=p_tenant AND link.project_id=document_project
    AND link.document_id=p_document) THEN
   RAISE EXCEPTION 'V23_PROFILE_CONSENT_REVOKED' USING ERRCODE='40001';
  END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM photo.model_use_links link
   JOIN photo.learning_versions version ON version.tenant_id=link.tenant_id
    AND version.project_id=link.project_id AND version.id=link.version_id
   JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
    AND profile.project_id=version.project_id AND profile.id=version.profile_id
   JOIN photo.version_eligibility state ON state.tenant_id=link.tenant_id
    AND state.project_id=link.project_id AND state.version_id=link.version_id
   WHERE link.tenant_id=p_tenant AND link.project_id=document_project
    AND link.document_id=p_document AND link.version_id=p_learning_version
    AND profile.active_version_id=version.id AND state.status='eligible'
    AND photo.v23_learning_version_consents_live(p_tenant,p_learning_version)) THEN
   RAISE EXCEPTION 'V23_PROFILE_CONSENT_REVOKED' USING ERRCODE='40001';
  END IF;
 END IF;
 RETURN result;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.approve_v23_draft(uuid,uuid,bigint,uuid,uuid,uuid,uuid,text,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.approve_v23_draft(uuid,uuid,bigint,uuid,uuid,uuid,uuid,text,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.approve_v23_draft(uuid,uuid,bigint,uuid,uuid,uuid,uuid,text,uuid)
 TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
