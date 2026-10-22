-- +goose Up
-- Read-only recovery for the exact saved request and original editor session.
-- Never reports an absent/inaccessible row as evidence that commit rolled back.
-- +goose StatementBegin
CREATE FUNCTION photo.inspect_semantic_background_cleanup_preview(p_tenant uuid,p_preview uuid,
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
 OR r.config_snapshot->'semantic_background_cleanup_preview'->'params' IS DISTINCT FROM e
 OR r.config_snapshot->'semantic_background_cleanup_preview'->>'params_sha' IS DISTINCT FROM p_sha
 OR r.config_snapshot->'semantic_background_cleanup_preview'->>'actor_id' IS DISTINCT FROM p_actor::text
 OR r.config_snapshot->'semantic_background_cleanup_preview'->>'session_id' IS DISTINCT FROM p_session::text
 OR photo.semantic_preapply_preview_current(p_tenant,(e->>'intent_id')::uuid,(e->>'target_id')::uuid,
 p_actor,p_session,(e->>'workflow_revision')::bigint,(e->>'target_revision')::bigint,
 e->'plan'->>'sha256',e->'candidate'->>'sha256') IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INSPECTION_UNAVAILABLE' USING ERRCODE='42501';END IF;
 owner:=photo.semantic_uuid_v5(r.id,'semantic.preview_background_cleanup:'||(e->>'target_id')::uuid::text);
 RETURN QUERY SELECT r.status,coalesce(t.state,''),r.graph_sealed,t.id IS NOT NULL
 FROM (SELECT 1) singleton LEFT JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=r.id AND t.id=owner;
END $$;
ALTER FUNCTION photo.inspect_semantic_background_cleanup_preview(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.inspect_semantic_background_cleanup_preview(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.inspect_semantic_background_cleanup_preview(uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
