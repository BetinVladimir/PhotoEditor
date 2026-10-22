-- +goose Up
-- Reauthorize and lock the selected canonical preview before installing copy
-- Params. Caller must prove ApprovedSemanticEdit against the decision batch,
-- register full closure/quota and activate the child in the same transaction.
-- No task, approval or child is created by this private precondition.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_shown_exposure_params(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
 p_child uuid,p_target uuid,p_revision bigint,p_target_revision bigint,p_plan_sha text,p_candidate_sha text,
 p_body text,p_sha text,p_approved jsonb) RETURNS TABLE(preview_id uuid,receipt jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;selection record;preview_params jsonb;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_sha THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_PARAMS_INVALID' USING ERRCODE='22023';END IF;
 e:=p_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object'
 OR jsonb_typeof(e->'working_image') IS DISTINCT FROM 'object'
 OR jsonb_typeof(e->'presentation') IS DISTINCT FROM 'object'
 OR coalesce(e->'working_image'->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR coalesce(e->'presentation'->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_PARAMS_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO selection FROM photo.lock_semantic_preapply_preview_selection(p_tenant,p_intent,p_actor,p_session,
 p_revision,p_target,p_target_revision,p_plan_sha,p_candidate_sha,
 (e->'working_image'->>'artifact_id')::uuid,e->'working_image'->>'sha256',
 (e->'presentation'->>'artifact_id')::uuid,e->'presentation'->>'sha256');
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_SELECTION_STALE' USING ERRCODE='40001';END IF;
 SELECT r.config_snapshot->'semantic_preapply_preview'->'params' INTO preview_params
 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=selection.preview_id;
 IF NOT FOUND OR preview_params->>'intent_id' IS DISTINCT FROM p_intent::text
 OR p_child::text IS NOT DISTINCT FROM preview_params->>'workflow_id'
 OR photo.semantic_shown_exposure_params_bound(p_tenant,p_child,p_target,selection.preview_id,
 p_approved,e,preview_params,selection.receipt) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_PARAMS_INVALID' USING ERRCODE='22023';END IF;
 preview_id:=selection.preview_id;receipt:=selection.receipt;RETURN NEXT;
END $$;
ALTER FUNCTION photo.lock_semantic_shown_exposure_params(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_shown_exposure_params(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_shown_exposure_params(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,text,text,jsonb)
 TO photo_semantic_admission;
-- +goose StatementEnd
