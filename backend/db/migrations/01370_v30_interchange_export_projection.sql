-- +goose Up
-- Extend job projection to exports while preserving the existing import branch.
-- Adopted format survives pending-ledger cleanup in immutable evidence.
-- +goose StatementBegin
ALTER FUNCTION photo.get_v30_interchange(uuid,uuid,uuid,uuid) RENAME TO get_v30_interchange_pre_export;
REVOKE ALL ON FUNCTION photo.get_v30_interchange_pre_export(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api;
CREATE FUNCTION photo.get_v30_interchange(p_tenant uuid,p_actor uuid,p_session uuid,p_job uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(photo.get_v30_interchange_pre_export(p_tenant,p_actor,p_session,p_job),(
 SELECT jsonb_build_object('id',w.id,'revision',r.revision::text,'document',NULL,
 'data',jsonb_build_object('resource_type','interchange_jobs','id',w.id,'tenant_id',w.tenant_id,'project_id',w.project_id,
 'created_at',r.created_at,'updated_at',coalesce((SELECT max(event.created_at) FROM photo.run_events event WHERE event.tenant_id=r.tenant_id AND event.run_id=r.id),r.created_at),
 'revision',r.revision::text,'workflow_id',w.id,'direction','export','format',coalesce(e.body->>'format',pending.response_body#>>'{frozen,request,format}'),
 'mapping_artifact_id',NULL,'report_artifact_id',CASE WHEN readiness.ready THEN e.body#>>'{documents,1,artifact_id}' ELSE NULL END,
 'bundle_artifact_id',CASE WHEN readiness.ready THEN e.body#>>'{bundle,artifact_id}' ELSE NULL END))
 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authorized
 JOIN photo.v2_workflows w ON w.tenant_id=p_tenant AND w.id=p_job AND w.workflow_type='interchange' AND w.renderer_version='v2.2'
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id AND r.kind='v2' AND r.graph_sealed AND r.status<>'draft'
 JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id AND p.archived_at IS NULL
 JOIN photo.tasks task ON task.tenant_id=w.tenant_id AND task.run_id=r.id AND task.id=photo.v30_uuid5(w.id,'v2.interchange.export:0')
 AND task.operation='v2.interchange.export' AND task.operation_version='1'
 JOIN photo.backend_documents d ON d.tenant_id=task.tenant_id AND d.owner_id=task.id AND d.artifact_id=task.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 AND d.v30_interchange_proof->>'direction'='export' AND d.v30_interchange_proof->>'actor_id'=r.created_by::text
 AND d.v30_interchange_proof->>'request_sha'=r.config_snapshot->>'request_sha'
 LEFT JOIN photo.idempotency_records pending ON pending.tenant_id=r.tenant_id AND pending.principal_type='user' AND pending.principal_id=r.created_by
 AND pending.route='Interchange.Export.Pending' AND pending.key=d.v30_interchange_proof->>'key' AND pending.request_hash=r.config_snapshot->>'request_sha'
 LEFT JOIN photo.v30_interchange_export_evidence proof ON proof.tenant_id=r.tenant_id AND proof.run_id=r.id AND proof.result_id=task.canonical_result_id
 LEFT JOIN LATERAL(SELECT proof.evidence_jcs::jsonb body) e ON proof.run_id IS NOT NULL
 LEFT JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.id=task.canonical_result_id AND result.task_id=task.id AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token
 CROSS JOIN LATERAL(SELECT coalesce(p.download_allowed AND r.status IN('running','completed') AND NOT r.cancel_requested
 AND task.state='succeeded' AND NOT task.cancel_requested AND result.outputs->0=e.body->'root'
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=r.tenant_id AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(task.id,result.id))
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=r.tenant_id AND consumer='v30-interchange-export' AND event_id=photo.v2_adoption_event_id(task.id,result.id))
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(result.outputs) ref WHERE NOT EXISTS(
 SELECT 1 FROM photo.artifacts a JOIN photo.v2_artifact_links link ON link.tenant_id=a.tenant_id AND link.artifact_id=a.id AND link.sha256=a.sha256
 WHERE a.tenant_id=r.tenant_id AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256' AND a.created_by_task_id=task.id
 AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND link.workflow_id=w.id AND link.project_id=w.project_id)),false) ready) readiness
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid AND authorized.role IN('owner','admin','editor','viewer')
 AND (authorized.role IN('owner','admin') OR p.visibility='workspace' OR EXISTS(SELECT 1 FROM photo.project_acl acl
 WHERE acl.tenant_id=p_tenant AND acl.project_id=p.id AND acl.user_id=p_actor AND acl.role IN('editor','viewer')))
 AND coalesce(e.body->>'format',pending.response_body#>>'{frozen,request,format}') IN('photoeditor_bundle','xmp','captureone_sidecar')
 ))
$$;
ALTER FUNCTION photo.get_v30_interchange(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v30_interchange(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.get_v30_interchange(uuid,uuid,uuid,uuid) TO photo_api;
-- Existing inspect is SQL STABLE; replace its export stage after ordinary read
-- authorization and keep all prior import lifecycle labels intact.
ALTER FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) RENAME TO inspect_v30_interchange_pre_export;
REVOKE ALL ON FUNCTION photo.inspect_v30_interchange_pre_export(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api;
CREATE FUNCTION photo.inspect_v30_interchange(p_tenant uuid,p_actor uuid,p_session uuid,p_job uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT CASE WHEN job#>>'{data,direction}'='export' THEN original||jsonb_build_object('stage',CASE
 WHEN original->>'status' IN('completed','completed_with_errors') THEN 'completed'
 WHEN original->>'status' IN('failed','cancelled','cancelling','queued') THEN original->>'status'
 WHEN job#>>'{data,bundle_artifact_id}' IS NOT NULL THEN 'finalizing' ELSE 'exporting' END)
 ELSE original END
 FROM (SELECT photo.get_v30_interchange(p_tenant,p_actor,p_session,p_job) job,photo.inspect_v30_interchange_pre_export(p_tenant,p_actor,p_session,p_job) original) scoped
 WHERE job IS NOT NULL
$$;
ALTER FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
