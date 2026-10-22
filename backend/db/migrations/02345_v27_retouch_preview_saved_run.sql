-- +goose Up
-- Authenticated immutable draft intent only. No task dispatch, quota bypass or Apply.
-- +goose StatementBegin
CREATE FUNCTION photo.save_semantic_retouch_preview(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,
 p_body text,p_sha text,p_context uuid,p_context_sha text,p_context_body text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb; parent uuid; identity record; target photo.semantic_targets%ROWTYPE;
 preview photo.runs%ROWTYPE; config jsonb; preview_id uuid; context_doc jsonb;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_sha THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INVALID' USING ERRCODE='22023';END IF;
 IF p_context IS NULL OR p_context='00000000-0000-0000-0000-000000000000'::uuid
 OR p_context_sha IS NULL OR p_context_sha !~ '^[0-9a-f]{64}$'
 OR p_context_body IS NULL OR octet_length(p_context_body) NOT BETWEEN 2 AND 1048576
 OR encode(public.digest(convert_to(p_context_body,'UTF8'),'sha256'),'hex')<>p_context_sha THEN
 RAISE EXCEPTION 'SEMANTIC_RETOUCH_CONTEXT_INVALID' USING ERRCODE='22023';END IF;
 e:=p_body::jsonb;context_doc:=p_context_body::jsonb;
 IF jsonb_typeof(context_doc) IS DISTINCT FROM 'object'
 OR context_doc->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR context_doc->>'algorithm' IS DISTINCT FROM 'beautyfilter_v1'
 OR context_doc->'source' IS DISTINCT FROM e->'source'
 OR context_doc->'candidate' IS DISTINCT FROM e->'candidate'
 OR context_doc->'protection' IS DISTINCT FROM e->'protection'
 OR context_doc->>'model_lock_sha' IS DISTINCT FROM e->>'model_lock_sha'
 OR context_doc->>'runtime_lock_sha' IS DISTINCT FROM e->>'runtime_lock_sha'
 OR context_doc->>'device_profile_sha' IS DISTINCT FROM e->>'device_profile_sha'
 OR jsonb_typeof(context_doc->'faces') IS DISTINCT FROM 'array'
 OR EXISTS(SELECT 1 FROM (VALUES(e->'plan'),(e->'intent'),(e->'candidate'),(e->'source'->'source'),(e->'protection'),(context_doc->'transferred_mask')) roots(ref)
 WHERE roots.ref->>'artifact_id'=p_context::text) THEN
 RAISE EXCEPTION 'SEMANTIC_RETOUCH_CONTEXT_INVALID' USING ERRCODE='22023';END IF;
 IF jsonb_array_length(context_doc->'faces') NOT BETWEEN 1 AND 64 THEN
 RAISE EXCEPTION 'SEMANTIC_RETOUCH_CONTEXT_INVALID' USING ERRCODE='22023';END IF;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object' OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR e->>'kind' IS DISTINCT FROM 'retouch_region'
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
 OR photo.semantic_preapply_preview_current(p_tenant,p_intent,p_target,p_actor,p_session,
  (e->>'workflow_revision')::bigint,(e->>'target_revision')::bigint,
  e->'plan'->>'sha256',e->'candidate'->>'sha256') IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_STALE' USING ERRCODE='40001';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_review_plan_sources(p_tenant,identity.workflow_id,p_actor,p_session) source
 WHERE source.contract='SemanticPlan' AND source.artifact_id::text=e->'plan'->>'artifact_id'
 AND source.sha256=e->'plan'->>'sha256')
 OR NOT EXISTS(SELECT 1 FROM photo.semantic_intents intent WHERE intent.tenant_id=p_tenant
 AND intent.id=p_intent AND intent.snapshot_id::text=e->>'snapshot_id'
 AND intent.intent_artifact_id::text=e->'intent'->>'artifact_id'
 AND intent.intent_sha=e->'intent'->>'sha256') THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_STALE' USING ERRCODE='40001';END IF;
 preview_id:=photo.semantic_uuid_v5(parent,'semantic-retouch-preview:'||p_target::text||':'||p_sha||':'||p_context::text||':'||p_context_sha||':'||p_actor::text||':'||p_session::text);
 config:=jsonb_build_object('semantic_retouch_preview',jsonb_build_object(
  'params',e,'params_sha',p_sha,'actor_id',p_actor,'session_id',p_session,
  'adapter_context_ref',jsonb_build_object('artifact_id',p_context,'sha256',p_context_sha),'adapter_context',context_doc,'adapter_context_body',p_context_body));
 SELECT * INTO preview FROM photo.runs WHERE id=preview_id FOR UPDATE;
 IF FOUND THEN
  IF preview.tenant_id=p_tenant AND preview.kind='preview' AND NOT preview.cancel_requested
   AND preview.created_by=p_actor AND preview.config_snapshot=config
   AND preview.input_snapshot_id::text=e->>'snapshot_id' THEN RETURN preview_id;END IF;
  RAISE EXCEPTION 'SEMANTIC_PREVIEW_CONFLICT' USING ERRCODE='40001';
 END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed)
 VALUES(preview_id,p_tenant,'preview','draft',(e->>'snapshot_id')::uuid,config,
  encode(public.digest(convert_to(config::text,'UTF8'),'sha256'),'hex'),p_actor,false);
 RETURN preview_id;
END $$;
ALTER FUNCTION photo.save_semantic_retouch_preview(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_semantic_retouch_preview(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.save_semantic_retouch_preview(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
