-- +goose Up
-- Session receipt belongs to the activated signing Params, not task JSON.
ALTER TABLE photo.backend_documents ADD COLUMN v31_signing_session_id uuid;
-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_session_immutable() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.v31_signing_session_id IS NOT NULL AND NEW.v31_signing_session_id IS DISTINCT FROM OLD.v31_signing_session_id THEN
  RAISE EXCEPTION 'Immutable signing activation session' USING ERRCODE='23514';
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER v31_signing_session_immutable BEFORE UPDATE ON photo.backend_documents
 FOR EACH ROW EXECUTE FUNCTION photo.v31_signing_session_immutable();
ALTER FUNCTION photo.v31_signing_session_immutable() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_session_immutable() FROM PUBLIC;
ALTER FUNCTION photo.activate_v31_signing(uuid,uuid,uuid,text,text,jsonb,jsonb) RENAME TO activate_v31_signing_pre_session;
REVOKE ALL ON FUNCTION photo.activate_v31_signing_pre_session(uuid,uuid,uuid,text,text,jsonb,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.activate_v31_signing(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_task jsonb,p_manifest jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE activated boolean;
BEGIN
 activated:=photo.activate_v31_signing_pre_session(p_tenant,p_actor,p_session,p_key,p_request_sha,p_task,p_manifest);
 IF activated IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.backend_documents d SET v31_signing_session_id=p_session
 WHERE d.tenant_id=p_tenant AND d.owner_id=(p_task->>'id')::uuid AND d.purpose='task_params'
 AND d.artifact_id=(p_task->>'params_artifact_id')::uuid AND d.state='ready' AND d.v31_signing_session_id IS NULL;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant
  AND d.owner_id=(p_task->>'id')::uuid AND d.purpose='task_params' AND d.artifact_id=(p_task->>'params_artifact_id')::uuid
  AND d.state='ready' AND d.v31_signing_session_id IS NOT NULL) THEN
  RAISE EXCEPTION 'Signing activation session missing' USING ERRCODE='23514';
 END IF;RETURN true;
END $$;
ALTER FUNCTION photo.activate_v31_signing(uuid,uuid,uuid,text,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v31_signing(uuid,uuid,uuid,text,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.activate_v31_signing(uuid,uuid,uuid,text,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_adoption_params(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id
  AND d.purpose='task_params' AND d.artifact_id=t.params_artifact_id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
 JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='Params'
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id
 JOIN photo.projects project ON project.tenant_id=w.tenant_id AND project.id=w.project_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='provenance.sign' AND t.operation_version='1'
 AND t.resource_class='cpu' AND t.execution_profile->>'execution_pool'='signer' AND t.execution_profile->>'backend'='cpu'
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.canonical_result_id=p_result
 AND z.attempt=p_attempt AND z.fencing_token=p_fence
 AND tenant.status='active' AND project.archived_at IS NULL AND project.download_allowed
 AND r.kind='v2' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND w.workflow_type='provenance' AND w.renderer_version='v2.2'
 AND d.v31_signing_session_id IS NOT NULL AND photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,d.v31_signing_session_id)
 AND d.state='ready' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.output_role='task_params' AND a.bytes=d.bytes AND a.bytes BETWEEN 2 AND 65536 AND a.bucket=d.bucket
 AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND a.object_key='tenants/'||p_tenant||'/runs/'||r.id||'/documents/'||a.id
 AND r.config_snapshot->'params'=jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v2_input_closure_ready(t.id) AND EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task))
$$;
ALTER FUNCTION photo.v31_signing_adoption_params(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_adoption_params(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v31_signing_adoption_params(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- Generic inventory adoption cannot publish signer output without dedicated
-- independent verification and provenance insertion in the same transaction.
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_v31_signer;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_v31_signer(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='provenance.sign') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_v31_signer(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
