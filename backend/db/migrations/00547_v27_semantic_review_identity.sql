-- +goose Up
-- Minimal authenticated identity for request replay, independent of artifact
-- expiry, mutable producer generations and parent execution status.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_review_identity(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(run_id uuid,workflow_id uuid,project_id uuid)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RETURN QUERY SELECT w.run_id,w.id,i.project_id
 FROM photo.semantic_intents i JOIN photo.v2_workflows w
 ON w.tenant_id=i.tenant_id AND w.project_id=i.project_id AND w.id=i.workflow_id
 WHERE i.tenant_id=p_tenant AND i.id=p_intent AND w.workflow_type='semantic'
 AND photo.v2_editor_authorized(p_tenant,i.project_id,p_actor,p_session);
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_REVIEW_FORBIDDEN' USING ERRCODE='42501';END IF;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.semantic_review_identity(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_review_identity(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_review_identity(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
