-- +goose Up
-- Face documents are authorized only through a current server-issued confirmation.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_faces_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_actor uuid,p_session uuid,p_confirmation uuid,p_confirmation_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE context_value jsonb;base jsonb;
BEGIN
 SELECT d.v28_donor_issuer_context INTO context_value FROM photo.backend_documents d
 WHERE d.tenant_id=p_tenant AND d.artifact_id=p_confirmation AND d.sha256=p_confirmation_sha
  AND d.purpose='v28_donor_confirmation' AND d.state='ready';
 IF NOT FOUND THEN RETURN false;END IF;
 base:=context_value->'params'->'base';
 IF NOT ((context_value->'params'->'source_faces'->>'artifact_id'=p_artifact::text
   AND context_value->'params'->'source_faces'->>'sha256'=p_sha
   AND base->>'asset_version_id'=p_version::text)
  OR (context_value->'params'->'donor_faces'->>'artifact_id'=p_artifact::text
   AND context_value->'params'->'donor_faces'->>'sha256'=p_sha
   AND context_value->'params'->'donor'->>'asset_version_id'=p_version::text)) IS TRUE THEN RETURN false;END IF;
 -- This repeats both the ready issuer lineage and current observation/source locks.
 RETURN photo.v28_donor_confirmation_scope(p_tenant,p_project,(base->>'asset_version_id')::uuid,
  p_confirmation,p_confirmation_sha,p_actor,p_session,(base->'source'->>'artifact_id')::uuid,base->'source'->>'sha256');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_donor_faces_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_faces_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_donor_faces_scope(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text) TO photo_api;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) RENAME TO v2_draft_artifact_scope_pre_v28_faces;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope_pre_v28_faces(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM photo_api;
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_contract='DonorFaceObservations' THEN RETURN photo.v28_donor_faces_scope(p_tenant,p_project,p_version,p_artifact,p_sha,p_actor,p_session,p_parent,p_parent_sha);END IF;
 RETURN photo.v2_draft_artifact_scope_pre_v28_faces(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,p_parent,p_parent_sha);
END $$;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
