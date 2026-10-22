-- +goose Up
-- Result-scoped scheduler authority for retouch-copy adoption.
-- Result-scoped Params lookup only; this does not authorize final adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_shown_retouch_adoption_params(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.tasks t
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.kind='apply'
 AND r.graph_sealed AND NOT r.cancel_requested AND r.status IN('queued','running')
 JOIN photo.projects p ON p.tenant_id=r.tenant_id AND p.id=(r.config_snapshot->>'project_id')::uuid AND p.archived_at IS NULL
 JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.task_id=t.id
 AND result.id=t.canonical_result_id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='semantic.adopt_shown_retouch_region'
 AND t.operation_version='1' AND t.resource_class='cpu' AND t.state='succeeded'
 AND NOT t.cancel_requested AND t.attempt=p_attempt AND t.fencing_token=p_fence AND result.id=p_result
 AND t.execution_profile->>'backend'='cpu' AND t.execution_profile->>'renderer_version'='v2.2'
 AND jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes')='array'
 AND (SELECT count(*) FROM jsonb_array_elements(CASE WHEN jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes')='array'
 THEN r.config_snapshot->'shown_semantic'->'nodes' ELSE '[]'::jsonb END) n
 WHERE n->>'task_id'=t.id::text AND n->>'params_artifact_id'=a.id::text AND n->>'params_sha'=a.sha256)=1;
$$;
ALTER FUNCTION photo.v27_shown_retouch_adoption_params(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_retouch_adoption_params(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.v27_shown_retouch_adoption_params(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd

-- Discover only canonical retouch-copy results bound to their committed review child.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_shown_retouch_adoption_context(p_tenant uuid,p_task uuid)
RETURNS TABLE(attempt integer,fence bigint,result_id uuid,operation text,operation_version text,
 root_artifact_id uuid,root_sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.attempt,t.fencing_token,z.id,t.operation,t.operation_version,
  (z.outputs->0->>'artifact_id')::uuid,z.outputs->0->>'sha256'
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.state='succeeded'
 AND r.kind='apply' AND r.status='running' AND NOT r.cancel_requested AND NOT t.cancel_requested
 AND tenant.status='active'
 AND t.operation='semantic.adopt_shown_retouch_region' AND t.operation_version='1' AND t.resource_class='cpu'
 AND EXISTS(SELECT 1 FROM photo.v27_shown_retouch_adoption_params(p_tenant,p_task,t.attempt,t.fencing_token,z.id))
 AND t.execution_profile->>'renderer_version'='v2.2'
 AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token;
$$;

ALTER FUNCTION photo.v27_shown_retouch_adoption_context(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_retouch_adoption_context(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.v27_shown_retouch_adoption_context(uuid,uuid) TO photo_scheduler;
CREATE FUNCTION photo.v27_pending_shown_retouch_adoptions() RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.run_id,t.id FROM photo.tasks t
 CROSS JOIN LATERAL photo.v27_shown_retouch_adoption_context(t.tenant_id,t.id) c
 WHERE t.operation='semantic.adopt_shown_retouch_region' AND t.operation_version='1'
 AND NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
 AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,c.result_id))
 ORDER BY t.created_at,t.id LIMIT 64;
$$;
ALTER FUNCTION photo.v27_pending_shown_retouch_adoptions() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_pending_shown_retouch_adoptions() FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.v27_pending_shown_retouch_adoptions() TO photo_scheduler;
-- +goose StatementEnd

-- Read-phase receipt admission; final publication must repeat under locks.
-- No running worker lease is required after a canonical successful result.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_shown_retouch_adoption_ready(p_tenant uuid,p_task uuid,p_attempt integer,
 p_fence bigint,p_result uuid,p_params uuid,p_sha text,p_body text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 e jsonb;old_preview photo.runs%ROWTYPE;producer photo.tasks%ROWTYPE;project uuid;actor uuid;session_id uuid;field text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 OR p_sha IS NULL
 OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_sha
 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v27_shown_retouch_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result) a
 WHERE a.artifact_id=p_params AND a.sha256=p_sha) THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND bytes=octet_length(p_body))
 THEN RETURN false;END IF;
 e:=p_body::jsonb;
 SELECT * INTO old_preview FROM photo.runs WHERE tenant_id=p_tenant AND id=(e->>'preview_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR old_preview.kind IS DISTINCT FROM 'preview' OR NOT old_preview.graph_sealed OR old_preview.created_by IS DISTINCT FROM r.created_by
 OR photo.semantic_shown_retouch_params_bound(p_tenant,t.run_id,(e->>'target_id')::uuid,old_preview.id,
 e->'approved_edit',e,old_preview.config_snapshot->'semantic_retouch_preview'->'params',old_preview.v27_retouch_preview_receipt) IS DISTINCT FROM true
 OR photo.semantic_shown_retouch_closure_ready(p_task,e) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOREACH field IN ARRAY ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha'] LOOP
 IF e->field IS DISTINCT FROM t.execution_profile->field THEN RETURN false;END IF;
 END LOOP;
 actor:=r.created_by;session_id:=(old_preview.config_snapshot->'semantic_retouch_preview'->>'session_id')::uuid;
 SELECT i.project_id INTO project FROM photo.semantic_targets target
 JOIN photo.semantic_intents i ON i.tenant_id=target.tenant_id AND i.id=target.intent_id
 JOIN photo.v2_review_batches b ON b.tenant_id=target.tenant_id AND b.id=target.batch_id
 WHERE target.tenant_id=p_tenant AND target.id=(e->>'target_id')::uuid AND target.state='queued'
 AND target.child_run_id=t.run_id AND b.child_run_id=t.run_id AND b.actor_id=actor AND b.action='apply'
 AND b.workflow_id=i.workflow_id AND b.project_id=i.project_id AND i.snapshot_id=r.input_snapshot_id
 AND i.id::text=old_preview.config_snapshot->'semantic_retouch_preview'->'params'->>'intent_id';
 IF NOT FOUND OR r.config_snapshot->>'project_id' IS DISTINCT FROM project::text
 OR photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS DISTINCT FROM true
 OR photo.v27_downstream_profile_ready(p_tenant,project,'semantic.adopt_shown_retouch_region','1','cpu',t.execution_profile) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=old_preview.id AND id=(e->>'preview_task_id')::uuid FOR SHARE;
 IF NOT FOUND OR producer.state IS DISTINCT FROM 'succeeded'
 OR producer.operation IS DISTINCT FROM 'semantic.preview_retouch_region' OR producer.operation_version IS DISTINCT FROM '1'
 OR producer.resource_class IS DISTINCT FROM 'cpu'
 OR photo.semantic_retouch_preview_bundle_ready(producer.id,producer.canonical_result_id,producer.attempt,producer.fencing_token) IS DISTINCT FROM true
 OR producer.canonical_result_id::text IS DISTINCT FROM e->>'preview_result_id'
 OR producer.attempt::text IS DISTINCT FROM e->>'preview_attempt' OR producer.fencing_token::text IS DISTINCT FROM e->>'preview_fence'
 OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=producer.canonical_result_id
 AND z.task_id=producer.id AND z.attempt=producer.attempt AND z.fencing_token=producer.fencing_token
 AND z.outputs=jsonb_build_array(e->'presentation')) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=t.run_id
 AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=actor AND q.share_id IS NULL AND q.files=0
 AND q.bytes>= (t.execution_profile->'resource_estimate'->>'output_bytes')::bigint AND q.expires_at>clock_timestamp()) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v27_shown_retouch_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result) a
 WHERE a.artifact_id=p_params AND a.sha256=p_sha);
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v27_shown_retouch_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_retouch_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.v27_shown_retouch_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd

-- Coordinates and complete acceptance policy are exposed only through the
-- exact completed shown-copy result. No object IO occurs inside this statement.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_shown_retouch_adoption_report_evidence(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_params uuid,p_sha text,p_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; p photo.v2_qualification_policies%ROWTYPE; report photo.artifacts%ROWTYPE;
BEGIN
 IF photo.v27_shown_retouch_adoption_ready(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_sha,p_body) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT t.execution_profile->'qualification' INTO pin
 FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.id=p_task;
 IF pin IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->'report'->>'artifact_id')::uuid;
 RETURN jsonb_build_object('pin',pin,'policy',jsonb_build_object(
 'schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,'backend',p.backend,
 'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,
 'dataset_sha',p.dataset_sha,'issuer_id',p.issuer_id,'required_checks',p.required_checks,'test_fixture_shas',p.test_fixture_shas,
 'calibration_shas',p.calibration_shas,'evidence_shas',p.evidence_shas,'license_cleared',p.license_cleared),
 'report',jsonb_build_object('artifact_id',report.id,'sha256',report.sha256,'bucket',report.bucket,
 'object_key',report.object_key,'object_version',report.object_version,'bytes',report.bytes,'media_type',report.media_type));
END $$;
ALTER FUNCTION photo.v27_shown_retouch_adoption_report_evidence(uuid,uuid,integer,bigint,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_retouch_adoption_report_evidence(uuid,uuid,integer,bigint,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.v27_shown_retouch_adoption_report_evidence(uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
