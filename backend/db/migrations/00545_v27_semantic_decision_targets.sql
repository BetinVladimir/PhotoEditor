-- +goose Up
-- API cannot UPDATE review tables directly. Decision admission obtains the
-- authenticated source fence before locking the complete target state inventory.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_decision_targets(
 p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid,p_revision bigint)
RETURNS TABLE(target_id uuid,asset_version_id uuid,candidate_artifact_id uuid,
 state text,target_revision bigint,child_run_id uuid,batch_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent photo.semantic_intents%ROWTYPE; n integer;
BEGIN
 SELECT * INTO intent FROM photo.semantic_intents i
 WHERE i.tenant_id=p_tenant AND i.workflow_id=p_workflow;
 IF intent.id IS NULL OR NOT photo.v2_editor_authorized(p_tenant,intent.project_id,p_actor,p_session) THEN
  RAISE EXCEPTION 'SEMANTIC_DECISION_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 IF NOT photo.lock_semantic_review_sources(p_tenant,p_workflow,p_actor,p_session,p_revision,intent.reference_version_id) THEN
  RAISE EXCEPTION 'SEMANTIC_DECISION_SCOPE_STALE' USING ERRCODE='40001';
 END IF;
 PERFORM 1 FROM photo.semantic_intents i WHERE i.tenant_id=p_tenant AND i.id=intent.id FOR UPDATE;
 PERFORM 1 FROM photo.semantic_targets t WHERE t.tenant_id=p_tenant AND t.intent_id=intent.id ORDER BY t.id FOR UPDATE;
 SELECT count(*) INTO n FROM photo.semantic_targets t WHERE t.tenant_id=p_tenant AND t.intent_id=intent.id;
 IF n NOT BETWEEN 1 AND 5000 OR NOT photo.v2_editor_authorized(p_tenant,intent.project_id,p_actor,p_session) THEN
  RAISE EXCEPTION 'SEMANTIC_DECISION_SCOPE_STALE' USING ERRCODE='40001';
 END IF;
 RETURN QUERY SELECT t.id,t.asset_version_id,t.candidate_artifact_id,t.state,t.revision,t.child_run_id,t.batch_id
 FROM photo.semantic_targets t WHERE t.tenant_id=p_tenant AND t.intent_id=intent.id ORDER BY t.id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.lock_semantic_decision_targets(uuid,uuid,uuid,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_decision_targets(uuid,uuid,uuid,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_decision_targets(uuid,uuid,uuid,uuid,bigint) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
