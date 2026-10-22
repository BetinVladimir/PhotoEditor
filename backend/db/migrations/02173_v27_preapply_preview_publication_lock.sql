-- +goose Up
-- Private transaction lock for saved preview publication; does not create a run,
-- approve a target, dispatch a task or grant public execution authority.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_preapply_preview_publication(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,
 p_preview uuid,p_body text,p_sha text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb; parent uuid; identity record; target photo.semantic_targets%ROWTYPE;
 preview photo.runs%ROWTYPE; config jsonb;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_sha
 OR p_preview IS NULL OR p_preview='00000000-0000-0000-0000-000000000000'::uuid THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INVALID' USING ERRCODE='22023';END IF;
 e:=p_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object' OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR e->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR e->>'intent_id' IS DISTINCT FROM p_intent::text OR e->>'target_id' IS DISTINCT FROM p_target::text
 OR e->>'workflow_revision' IS NULL OR e->>'workflow_revision' !~ '^[1-9][0-9]{0,18}$'
 OR e->>'target_revision' IS NULL OR e->>'target_revision' !~ '^[1-9][0-9]{0,18}$' THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INVALID' USING ERRCODE='22023';END IF;
 parent:=photo.lock_semantic_decision_publication(p_tenant,p_intent,p_actor,p_session,
  (e->>'workflow_revision')::bigint);
 SELECT * INTO identity FROM photo.semantic_review_identity(p_tenant,p_intent,p_actor,p_session);
 SELECT * INTO target FROM photo.semantic_targets
 WHERE tenant_id=p_tenant AND intent_id=p_intent AND id=p_target FOR UPDATE;
 IF target.id IS NULL OR target.state<>'proposed' OR target.child_run_id IS NOT NULL
 OR target.batch_id IS NOT NULL OR target.revision IS DISTINCT FROM (e->>'target_revision')::bigint
 OR target.asset_version_id::text IS DISTINCT FROM e->'source'->>'asset_version_id'
 OR target.candidate_artifact_id::text IS DISTINCT FROM e->'candidate'->>'artifact_id'
 OR e->>'workflow_id' IS DISTINCT FROM identity.workflow_id::text
 OR NOT photo.semantic_preapply_preview_current(p_tenant,p_intent,p_target,p_actor,p_session,
  (e->>'workflow_revision')::bigint,(e->>'target_revision')::bigint,
  e->'plan'->>'sha256',e->'candidate'->>'sha256') THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_STALE' USING ERRCODE='40001';END IF;
 SELECT * INTO preview FROM photo.runs WHERE tenant_id=p_tenant AND id=p_preview FOR UPDATE;
 config:=preview.config_snapshot->'semantic_preapply_preview';
 IF preview.id IS NULL OR preview.id=parent OR preview.kind<>'preview'
 OR preview.status<>'draft' OR preview.cancel_requested OR preview.graph_sealed
 OR preview.created_by IS DISTINCT FROM p_actor
 OR preview.input_snapshot_id::text IS DISTINCT FROM e->>'snapshot_id'
 OR jsonb_typeof(config) IS DISTINCT FROM 'object' OR config->'params' IS DISTINCT FROM e
 OR config->>'params_sha' IS DISTINCT FROM p_sha
 OR config->>'actor_id' IS DISTINCT FROM p_actor::text
 OR config->>'session_id' IS DISTINCT FROM p_session::text THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_STALE' USING ERRCODE='40001';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_review_plan_sources(p_tenant,identity.workflow_id,p_actor,p_session) source
 WHERE source.contract='SemanticPlan' AND source.artifact_id::text=e->'plan'->>'artifact_id'
 AND source.sha256=e->'plan'->>'sha256')
 OR NOT EXISTS(SELECT 1 FROM photo.semantic_intents intent WHERE intent.tenant_id=p_tenant
 AND intent.id=p_intent AND intent.snapshot_id=preview.input_snapshot_id
 AND intent.intent_artifact_id::text=e->'intent'->>'artifact_id'
 AND intent.intent_sha=e->'intent'->>'sha256') THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_STALE' USING ERRCODE='40001';END IF;
 RETURN p_preview;
END $$;
ALTER FUNCTION photo.lock_semantic_preapply_preview_publication(uuid,uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_preapply_preview_publication(uuid,uuid,uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
