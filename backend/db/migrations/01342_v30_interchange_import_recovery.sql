-- +goose Up
-- Historical acknowledgement of the atomic artifact commit; no fresh execution authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_import_adoption_committed(p_tenant uuid,p_task uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id AND w.workflow_type='interchange'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='v2.interchange.import' AND t.operation_version='1' AND t.state='succeeded'
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-interchange-import' AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 AND EXISTS(SELECT 1 FROM photo.run_events WHERE tenant_id=p_tenant AND run_id=t.run_id AND type='v2.task.adopted'
 AND payload->>'task_id'=t.id::text AND payload->>'result_id'=t.canonical_result_id::text AND payload->>'attempt'=t.attempt::text AND payload->>'fence'=t.fencing_token::text)
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
 WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id AND l.ordinal=0
 AND l.purpose=encode(sha256(convert_to('tasks/'||t.id::text||'/$','UTF8')),'hex')
 AND l.artifact_id=(z.outputs->0->>'artifact_id')::uuid AND l.sha256=z.outputs->0->>'sha256'
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND a.media_type='application/json' AND a.output_role='document'))
$$;
ALTER FUNCTION photo.v30_interchange_import_adoption_committed(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_adoption_committed(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_import_adoption_committed(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
