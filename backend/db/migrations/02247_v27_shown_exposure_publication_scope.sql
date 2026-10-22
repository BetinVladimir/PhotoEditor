-- +goose Up
-- Private pre-child document publication scope. This is not an Apply grant.
-- Tickets must repeat this lock before and after immutable object IO.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_shown_publication(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
 p_child uuid,p_target uuid,p_revision bigint,p_target_revision bigint,p_plan_sha text,p_candidate_sha text,
 p_body text,p_sha text,p_task uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent uuid;e jsonb;selected record;
BEGIN
 IF p_task IS NULL OR p_child IS NULL OR p_target IS NULL
 OR p_task IS DISTINCT FROM photo.semantic_uuid_v5(p_child,'semantic.adopt_shown_exposure:'||p_target::text)
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_PUBLICATION_INVALID' USING ERRCODE='22023';END IF;
 e:=p_body::jsonb;
 parent:=photo.lock_semantic_decision_publication(p_tenant,p_intent,p_actor,p_session,p_revision);
 IF parent IS NULL OR parent=p_child OR EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child)
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_PUBLICATION_STALE' USING ERRCODE='40001';END IF;
 SELECT * INTO selected FROM photo.lock_semantic_shown_exposure_params(p_tenant,p_intent,p_actor,p_session,
 p_child,p_target,p_revision,p_target_revision,p_plan_sha,p_candidate_sha,p_body,p_sha,e->'approved_edit');
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_PUBLICATION_STALE' USING ERRCODE='40001';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
 ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
 WHERE d.tenant_id=p_tenant AND d.run_id=parent AND d.owner_id=photo.semantic_uuid_v5(p_child,'semantic.edit:'||p_target::text)
 AND d.purpose='task_params' AND d.state='ready'
 AND d.artifact_id=(e->'approved_edit'->>'artifact_id')::uuid AND d.sha256=e->'approved_edit'->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version=d.object_version AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.bucket=d.bucket AND a.bytes=d.bytes AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_APPROVED_DOCUMENT_STALE' USING ERRCODE='40001';END IF;
 RETURN parent;
END $$;
ALTER FUNCTION photo.lock_semantic_shown_publication(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,text,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_shown_publication(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,text,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
