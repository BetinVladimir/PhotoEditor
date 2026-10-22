-- +goose Up
-- Private adoption precondition. Caller must atomically adopt returned bytes;
-- this helper never approves a target or schedules a fresh render.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_background_cleanup_preview_selection(p_tenant uuid,p_intent uuid,
 p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,
 p_plan_sha text,p_candidate_sha text,p_render uuid,p_render_sha text,p_evidence uuid,p_evidence_sha text)
RETURNS TABLE(preview_id uuid,receipt jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;e jsonb;sealed jsonb;matches integer:=0;found_run uuid;
BEGIN
 PERFORM photo.lock_semantic_decision_publication(p_tenant,p_intent,p_actor,p_session,p_revision);
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=p_tenant AND intent_id=p_intent AND id=p_target FOR UPDATE;
 IF photo.semantic_preapply_preview_current(p_tenant,p_intent,p_target,p_actor,p_session,
 p_revision,p_target_revision,p_plan_sha,p_candidate_sha) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_SELECTION_STALE' USING ERRCODE='40001';END IF;
 IF p_render IS NULL OR p_evidence IS NULL OR p_render=p_evidence
 OR p_render_sha IS NULL OR p_render_sha !~ '^[0-9a-f]{64}$'
 OR p_evidence_sha IS NULL OR p_evidence_sha !~ '^[0-9a-f]{64}$' THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_SELECTION_INVALID' USING ERRCODE='22023';END IF;
 FOR r IN SELECT x.* FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.kind='preview'
 AND x.status IN('running','completed') AND x.graph_sealed AND NOT x.cancel_requested AND x.created_by=p_actor
 AND x.config_snapshot->'semantic_background_cleanup_preview'->>'session_id'=p_session::text
 AND x.config_snapshot->'semantic_background_cleanup_preview'->'params'->>'intent_id'=p_intent::text
 AND x.config_snapshot->'semantic_background_cleanup_preview'->'params'->>'target_id'=p_target::text
 AND x.v27_background_cleanup_preview_receipt->'presentation'=jsonb_build_object('artifact_id',p_evidence,'sha256',p_evidence_sha)
 AND x.v27_background_cleanup_preview_receipt->'working_image'=jsonb_build_object('artifact_id',p_render,'sha256',p_render_sha)
 LIMIT 2 LOOP
 matches:=matches+1;found_run:=r.id;
 END LOOP;
 IF matches<>1 THEN RAISE EXCEPTION 'SEMANTIC_PREVIEW_SELECTION_UNAVAILABLE' USING ERRCODE='40001';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=found_run FOR UPDATE;
 e:=r.config_snapshot->'semantic_background_cleanup_preview'->'params';sealed:=r.v27_background_cleanup_preview_receipt;
 IF r.cancel_requested OR r.status NOT IN('running','completed') OR NOT r.graph_sealed
 OR e->>'workflow_revision' IS DISTINCT FROM p_revision::text
 OR e->>'target_revision' IS DISTINCT FROM p_target_revision::text
 OR e->'plan'->>'sha256' IS DISTINCT FROM p_plan_sha
 OR e->'candidate'->>'sha256' IS DISTINCT FROM p_candidate_sha
 OR sealed->>'params_sha' IS DISTINCT FROM r.config_snapshot->'semantic_background_cleanup_preview'->>'params_sha'
 OR sealed->'presentation' IS DISTINCT FROM jsonb_build_object('artifact_id',p_evidence,'sha256',p_evidence_sha)
 OR sealed->'working_image' IS DISTINCT FROM jsonb_build_object('artifact_id',p_render,'sha256',p_render_sha)
 OR photo.semantic_background_cleanup_preview_run_current(p_tenant,r.id) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_SELECTION_STALE' USING ERRCODE='40001';END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id
 AND id=photo.semantic_uuid_v5(r.id,'semantic.preview_background_cleanup:'||p_target::text) FOR UPDATE;
 IF NOT FOUND OR t.state<>'succeeded' OR t.cancel_requested
 OR t.id::text IS DISTINCT FROM sealed->>'task_id'
 OR t.canonical_result_id::text IS DISTINCT FROM sealed->>'result_id'
 OR t.attempt::text IS DISTINCT FROM sealed->>'attempt' OR t.fencing_token::text IS DISTINCT FROM sealed->>'fence'
 OR photo.semantic_background_cleanup_preview_closure_ready(t.id) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=t.canonical_result_id
 AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND z.outputs=jsonb_build_array(sealed->'presentation'))
 OR (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM (VALUES('preview_presentation',sealed->'presentation'),('working_image',sealed->'working_image'),
 ('pixels',sealed->'pixels'),('preview_evidence',sealed->'numerical_evidence')) expected(role,ref)
 WHERE a.output_role=expected.role AND a.id::text=expected.ref->>'artifact_id' AND a.sha256=expected.ref->>'sha256'))<>4 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_SELECTION_STALE' USING ERRCODE='40001';END IF;
 preview_id:=r.id;receipt:=sealed;RETURN NEXT;
END $$;
ALTER FUNCTION photo.lock_semantic_background_cleanup_preview_selection(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_background_cleanup_preview_selection(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_background_cleanup_preview_selection(uuid,uuid,uuid,uuid,bigint,uuid,bigint,text,text,uuid,text,uuid,text) TO photo_semantic_admission;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
