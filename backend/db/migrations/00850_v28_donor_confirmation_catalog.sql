-- +goose Up
-- Read authority is derived from ready server issuance and current live scope.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_confirmation_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_parent IS NULL OR coalesce(p_parent_sha,'') !~ '^[0-9a-f]{64}$'
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOR d IN SELECT doc.* FROM photo.backend_documents doc JOIN photo.artifacts a
  ON a.tenant_id=doc.tenant_id AND a.id=doc.artifact_id AND a.sha256=doc.sha256
  WHERE doc.tenant_id=p_tenant AND doc.artifact_id=p_artifact AND doc.sha256=p_sha
   AND doc.purpose='v28_donor_confirmation' AND doc.state='ready'
   AND doc.v28_donor_issuer_context->>'project_id'=p_project::text
   AND doc.v28_donor_issuer_context->>'actor_id'=p_actor::text
   AND doc.v28_donor_issuer_context->>'auth_session_id'=p_session::text
   AND doc.v28_donor_issuer_context->'params'->'base'->>'asset_version_id'=p_version::text
   AND doc.v28_donor_issuer_context->'params'->'base'->'source'->>'artifact_id'=p_parent::text
   AND doc.v28_donor_issuer_context->'params'->'base'->'source'->>'sha256'=p_parent_sha
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes=doc.bytes AND a.bucket=doc.bucket
   AND a.media_type='application/json' AND a.output_role='v28_donor_confirmation'
   AND a.object_version=doc.object_version AND a.object_version NOT IN('','null')
   AND a.object_key='tenants/'||p_tenant||'/runs/'||doc.run_id||'/documents/'||doc.artifact_id
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 LOOP
  IF photo.v28_donor_document_scope(p_tenant,d.run_id,d.owner_id,d.v28_donor_issuer_context,d.sha256,d.bytes) IS TRUE THEN RETURN true;END IF;
 END LOOP;
 RETURN false;
END $$;
ALTER FUNCTION photo.v28_donor_confirmation_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_confirmation_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_donor_confirmation_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text) TO photo_api;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) RENAME TO v2_draft_artifact_scope_pre_v28_donor;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope_pre_v28_donor(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM photo_api;
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_contract='DonorConfirmation' THEN
  RETURN photo.v28_donor_confirmation_scope(p_tenant,p_project,p_version,p_artifact,p_sha,p_actor,p_session,p_parent,p_parent_sha);
 END IF;
 RETURN photo.v2_draft_artifact_scope_pre_v28_donor(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,p_parent,p_parent_sha);
END $$;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
