-- +goose Up
-- Selected protection pins/producer only. Input-declaration barrier and policy/coverage remain caller obligations.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_protection_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_root uuid,p_root_sha text,p_child uuid,p_child_sha text,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE producer_id uuid; actual jsonb;
BEGIN
 IF jsonb_typeof(p_evidence) IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 IF jsonb_array_length(p_evidence)<>2 THEN RETURN false; END IF;
 IF photo.semantic_proposal_protection_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_root,p_root_sha,p_child,p_child_sha) IS NOT TRUE THEN RETURN false; END IF;
 SELECT created_by_task_id INTO producer_id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_root;
 IF producer_id IS NULL THEN RETURN false; END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer_id FOR SHARE;
 PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=producer_id ORDER BY attempt FOR SHARE;
 PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=producer_id ORDER BY id FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND (id=p_source OR created_by_task_id=producer_id OR id IN(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer_id)) ORDER BY id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer_id) FOR SHARE;
 PERFORM run_id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id IN(SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer_id) FOR SHARE;
 IF photo.semantic_proposal_protection_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_root,p_root_sha,p_child,p_child_sha) IS NOT TRUE THEN RETURN false; END IF;
 SELECT jsonb_agg(jsonb_build_object('role',pins.role,'artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) ORDER BY pins.role) INTO actual
 FROM (VALUES('protected_ref',p_root),('protected_pixels',p_child)) pins(role,id)
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=pins.id;
 RETURN coalesce(actual=p_evidence AND jsonb_array_length(actual)=2,false);
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
