-- +goose Up
-- Read fence only. No preview admission, publication, approval or Apply mutation.
-- Callers repeat after object IO; commit/adoption must lock the same live fences.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_current(
 p_tenant uuid,p_intent uuid,p_target uuid,p_actor uuid,p_session uuid,
 p_revision bigint,p_target_revision bigint,p_plan_sha text,p_candidate_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE identity record; plan_count integer; candidate_count integer;
BEGIN
 IF p_revision IS NULL OR p_revision<1 OR p_revision=9223372036854775807
 OR p_target_revision IS NULL OR p_target_revision<1 OR p_target_revision=9223372036854775807
 OR p_plan_sha IS NULL OR p_plan_sha !~ '^[0-9a-f]{64}$'
 OR p_candidate_sha IS NULL OR p_candidate_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT * INTO identity FROM photo.semantic_review_identity(p_tenant,p_intent,p_actor,p_session);
 IF identity.run_id IS NULL THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.runs r
  WHERE r.tenant_id=p_tenant AND r.id=identity.run_id AND r.revision=p_revision
  AND r.status='waiting_review' AND NOT r.cancel_requested) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_targets target
  JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id
   AND intent.project_id=target.project_id AND intent.id=target.intent_id
  JOIN photo.artifacts candidate ON candidate.tenant_id=target.tenant_id
   AND candidate.id=target.candidate_artifact_id
  WHERE target.tenant_id=p_tenant AND target.id=p_target AND target.intent_id=p_intent
  AND target.project_id=identity.project_id AND intent.workflow_id=identity.workflow_id
  AND target.revision=p_target_revision AND target.state='proposed'
  AND target.child_run_id IS NULL AND target.batch_id IS NULL
  AND candidate.sha256=p_candidate_sha AND candidate.state='ready'
  AND candidate.verified_at IS NOT NULL AND candidate.media_type='application/json'
  AND candidate.bytes BETWEEN 2 AND 1048576 AND candidate.object_version IS NOT NULL
  AND candidate.object_version NOT IN('','null')
  AND (candidate.expires_at IS NULL OR candidate.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 SELECT count(*) FILTER(WHERE source.contract='SemanticPlan' AND source.sha256=p_plan_sha),
  count(*) FILTER(WHERE source.contract='TransferCandidate' AND source.sha256=p_candidate_sha
   AND source.artifact_id=(SELECT candidate_artifact_id FROM photo.semantic_targets
    WHERE tenant_id=p_tenant AND id=p_target AND intent_id=p_intent))
 INTO plan_count,candidate_count
 FROM photo.semantic_review_plan_sources(p_tenant,identity.workflow_id,p_actor,p_session) source
 WHERE source.run_id=identity.run_id AND source.revision=p_revision;
 RETURN plan_count=1 AND candidate_count=1;
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_preapply_preview_current(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_current(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_preapply_preview_current(uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) TO photo_api;
-- +goose StatementEnd
