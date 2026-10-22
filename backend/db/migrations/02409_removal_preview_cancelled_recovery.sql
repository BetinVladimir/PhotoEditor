-- +goose Up
-- Recover an already sealed cancelled request; never resume a cancelled draft.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.save_semantic_removal_preview(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,
 p_body text,p_sha text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb; parent uuid; identity record; target photo.semantic_targets%ROWTYPE;
 preview photo.runs%ROWTYPE; config jsonb; preview_id uuid;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_sha THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INVALID' USING ERRCODE='22023';END IF;
 e:=p_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object' OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR e->>'kind' IS DISTINCT FROM 'remove_object' OR e->>'model_id' IS DISTINCT FROM 'big_lama'
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
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_review_plan_sources(p_tenant,identity.workflow_id,p_actor,p_session) source
 WHERE source.contract='SemanticPlan' AND source.artifact_id::text=e->'plan'->>'artifact_id'
 AND source.sha256=e->'plan'->>'sha256')
 OR NOT EXISTS(SELECT 1 FROM photo.semantic_intents intent WHERE intent.tenant_id=p_tenant
 AND intent.id=p_intent AND intent.snapshot_id::text=e->>'snapshot_id'
 AND intent.intent_artifact_id::text=e->'intent'->>'artifact_id'
 AND intent.intent_sha=e->'intent'->>'sha256') THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_STALE' USING ERRCODE='40001';END IF;
 preview_id:=photo.semantic_uuid_v5(parent,'semantic-removal-preview:'||p_target::text||':'||p_sha||':'||p_actor::text||':'||p_session::text);
 config:=jsonb_build_object('semantic_removal_preview',jsonb_build_object(
  'params',e,'params_sha',p_sha,'actor_id',p_actor,'session_id',p_session));
 SELECT * INTO preview FROM photo.runs WHERE id=preview_id FOR UPDATE;
 IF FOUND THEN
  IF preview.tenant_id=p_tenant AND preview.kind='preview'
   AND (NOT preview.cancel_requested OR (preview.graph_sealed AND EXISTS(
    SELECT 1 FROM photo.tasks existing WHERE existing.tenant_id=p_tenant AND existing.run_id=preview.id
    AND existing.id=photo.semantic_uuid_v5(preview.id,'semantic.preview_remove_object:'||p_target::text)
    AND existing.operation='semantic.preview_remove_object' AND existing.operation_version='1')))
   AND preview.created_by=p_actor AND preview.config_snapshot=config
   AND preview.input_snapshot_id::text=e->>'snapshot_id' THEN RETURN preview_id;END IF;
  RAISE EXCEPTION 'SEMANTIC_PREVIEW_CONFLICT' USING ERRCODE='40001';
 END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed)
 VALUES(preview_id,p_tenant,'preview','draft',(e->>'snapshot_id')::uuid,config,
  encode(public.digest(convert_to(config::text,'UTF8'),'sha256'),'hex'),p_actor,false);
 RETURN preview_id;
END $$;
ALTER FUNCTION photo.save_semantic_removal_preview(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_semantic_removal_preview(uuid,uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.save_semantic_removal_preview(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.inspect_semantic_removal_preview(p_tenant uuid,p_preview uuid,
 p_actor uuid,p_session uuid,p_body text,p_sha text)
RETURNS TABLE(run_status text,task_state text,graph_sealed boolean,task_present boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;e jsonb;owner uuid;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_sha THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INSPECTION_INVALID' USING ERRCODE='22023';END IF;
 e:=p_body::jsonb;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_preview;
 IF r.id IS NULL OR r.kind<>'preview' OR r.created_by IS DISTINCT FROM p_actor
 OR e->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR r.config_snapshot->'semantic_removal_preview'->'params' IS DISTINCT FROM e
 OR r.config_snapshot->'semantic_removal_preview'->>'params_sha' IS DISTINCT FROM p_sha
 OR r.config_snapshot->'semantic_removal_preview'->>'actor_id' IS DISTINCT FROM p_actor::text
 OR r.config_snapshot->'semantic_removal_preview'->>'session_id' IS DISTINCT FROM p_session::text
 OR photo.semantic_preapply_preview_current(p_tenant,(e->>'intent_id')::uuid,(e->>'target_id')::uuid,
 p_actor,p_session,(e->>'workflow_revision')::bigint,(e->>'target_revision')::bigint,
 e->'plan'->>'sha256',e->'candidate'->>'sha256') IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INSPECTION_UNAVAILABLE' USING ERRCODE='42501';END IF;
 owner:=photo.semantic_uuid_v5(r.id,'semantic.preview_remove_object:'||(e->>'target_id')::uuid::text);
 RETURN QUERY SELECT CASE WHEN r.cancel_requested AND r.status NOT IN('completed','failed','cancelled') THEN 'cancelling'::text ELSE r.status END,coalesce(t.state,''),r.graph_sealed,t.id IS NOT NULL
 FROM (SELECT 1) singleton LEFT JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=r.id AND t.id=owner;
END $$;
ALTER FUNCTION photo.inspect_semantic_removal_preview(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.inspect_semantic_removal_preview(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.inspect_semantic_removal_preview(uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
