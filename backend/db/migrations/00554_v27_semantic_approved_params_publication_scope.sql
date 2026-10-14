-- +goose Up
-- Private scope lock for future authenticated Params reservation/completion.
-- No service receives execution authority until its publication wrapper exists.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_approved_params_publication(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_version uuid,p_candidate uuid,p_candidate_sha text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent uuid; t photo.semantic_targets%ROWTYPE;
BEGIN
 IF p_target_revision IS NULL OR p_target_revision<1 OR p_candidate_sha IS NULL
 OR p_candidate_sha !~ '^[0-9a-f]{64}$' THEN
 RAISE EXCEPTION 'SEMANTIC_PARAMS_INVALID' USING ERRCODE='22023';END IF;
 parent:=photo.lock_semantic_decision_publication(p_tenant,p_intent,p_actor,p_session,p_revision);
 SELECT * INTO t FROM photo.semantic_targets WHERE tenant_id=p_tenant AND intent_id=p_intent AND id=p_target FOR UPDATE;
 IF t.id IS NULL OR t.state<>'proposed' OR t.revision IS DISTINCT FROM p_target_revision
 OR t.asset_version_id IS DISTINCT FROM p_version OR t.candidate_artifact_id IS DISTINCT FROM p_candidate
 OR t.child_run_id IS NOT NULL OR t.batch_id IS NOT NULL THEN
 RAISE EXCEPTION 'SEMANTIC_PARAMS_STALE' USING ERRCODE='40001';END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_candidate AND sha256=p_candidate_sha
 AND state='ready' AND verified_at IS NOT NULL AND media_type='application/json'
 AND object_version IS NOT NULL AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_PARAMS_STALE' USING ERRCODE='40001';END IF;
 RETURN parent;
END $$;
ALTER FUNCTION photo.lock_semantic_approved_params_publication(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_approved_params_publication(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
