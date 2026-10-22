-- +goose Up
-- Separate shown-copy lease admission; independent of proposal revisions. No producer is enabled by this migration.
-- +goose StatementBegin
CREATE FUNCTION photo.admit_semantic_shown_retouch(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;s photo.resource_slots%ROWTYPE;
 e jsonb;old_preview photo.runs%ROWTYPE;producer photo.tasks%ROWTYPE;project uuid;actor uuid;session_id uuid;field text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_sha
 OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF r.id IS NULL OR r.kind<>'apply' OR r.status NOT IN('queued','running')
 OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF t.id IS NULL OR t.operation<>'semantic.adopt_shown_retouch_region' OR t.operation_version<>'1'
 OR t.resource_class<>'cpu' OR t.state<>'running' OR t.cancel_requested
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.worker_id IS DISTINCT FROM p_worker OR t.params_artifact_id IS DISTINCT FROM p_params
 OR t.input_digest IS DISTINCT FROM p_digest OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF s.id IS NULL OR s.resource_class IS DISTINCT FROM 'cpu' OR s.fencing_token IS DISTINCT FROM p_fence
 OR s.lease_until IS NULL OR s.lease_until<=clock_timestamp()
 OR photo.worker_authorized(p_tenant,p_worker,'cpu',s.node_id,t.operation,'cpu',t.execution_profile->>'model_lock_id') IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM worker_id FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker
 AND db_principal=session_user AND enabled AND v2_renderer_versions ? 'v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_sha
 AND state='ready' AND verified_at IS NOT NULL AND media_type='application/json'
 AND bytes=octet_length(p_body) AND object_version IS NOT NULL AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 e:=p_body::jsonb;
 SELECT * INTO old_preview FROM photo.runs WHERE tenant_id=p_tenant AND id=(e->>'preview_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR old_preview.kind IS DISTINCT FROM 'preview' OR NOT old_preview.graph_sealed OR old_preview.created_by IS DISTINCT FROM r.created_by
 OR photo.semantic_shown_retouch_params_bound(p_tenant,p_run,(e->>'target_id')::uuid,old_preview.id,
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
 AND target.child_run_id=p_run AND b.child_run_id=p_run AND b.actor_id=actor AND b.action='apply'
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
 IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
 AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=actor AND q.share_id IS NULL AND q.files=0
 AND q.bytes>= (t.execution_profile->'resource_estimate'->>'output_bytes')::bigint AND q.expires_at>clock_timestamp()) THEN RETURN false;END IF;
 RETURN t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp() AND s.lease_until>clock_timestamp();
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.admit_semantic_shown_retouch(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_semantic_shown_retouch(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.admit_semantic_shown_retouch(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd

-- Coordinates and complete acceptance policy are exposed only through the
-- live leased shown-copy task. No object IO occurs inside this statement.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_shown_retouch_report_evidence(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; p photo.v2_qualification_policies%ROWTYPE; report photo.artifacts%ROWTYPE;
BEGIN
 IF photo.admit_semantic_shown_retouch(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT t.execution_profile->'qualification' INTO pin
 FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task;
 IF photo.v27_downstream_profile_ready(p_tenant,
 (SELECT (config_snapshot->>'project_id')::uuid FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run),
 'semantic.adopt_shown_retouch_region','1','cpu',
 (SELECT execution_profile FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task)) IS DISTINCT FROM true THEN RETURN NULL;END IF;
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
ALTER FUNCTION photo.v27_shown_retouch_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_retouch_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.v27_shown_retouch_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
