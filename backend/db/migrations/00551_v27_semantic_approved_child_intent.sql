-- +goose Up
-- Private content-pin scope; authenticated task/lease wrapper is still required.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_child_edit(p_tenant uuid,p_child uuid,p_target uuid,p_version uuid,p_candidate uuid,p_candidate_sha text,p_source uuid,p_source_sha text,p_intent_artifact uuid,p_intent_sha text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.semantic_approved_child_source(p_tenant,p_child,p_target,p_version,p_candidate,p_candidate_sha,p_source,p_source_sha)
 AND EXISTS(SELECT 1 FROM photo.semantic_targets t
 JOIN photo.semantic_intents i ON i.tenant_id=t.tenant_id AND i.project_id=t.project_id AND i.id=t.intent_id
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.intent_artifact_id AND a.sha256=i.intent_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE t.tenant_id=p_tenant AND t.id=p_target AND t.child_run_id=p_child AND t.state='queued'
 AND i.intent_artifact_id=p_intent_artifact AND i.intent_sha=p_intent_sha);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.semantic_approved_child_edit(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_child_edit(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
