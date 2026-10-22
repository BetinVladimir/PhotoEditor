-- +goose Up
-- Reader-first schema pin rollout. Preserve worker/lease/qualification authority.
-- Exact document wire binding is enforced separately by persisted closure/admission.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE claimed record;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;expected numeric;
BEGIN
 SELECT * INTO claimed FROM photo.claim_task_before_shown_retouch(p_task,p_attempt,p_worker,p_command);
 IF claimed.outcome='claimed' THEN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.operation='semantic.adopt_shown_retouch_region' THEN
 -- Apply runs do not enter the generic run_kind='v2' renderer guard.
 -- Lock the current registration through lease/start commit, after the base
 -- claim has established the exact worker and dispatch identity.
 PERFORM w.worker_id FROM photo.worker_registrations w
 WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker AND w.enabled
 AND w.db_principal=session_user AND w.resource_classes ? 'cpu' AND w.backends ? 'cpu'
 AND (w.qualified_operations->'semantic.adopt_shown_retouch_region') ? 'cpu'
 AND w.v2_renderer_versions ? 'v2.2'
 AND photo.capacity_policy_worker_fresh(w) FOR SHARE;
 IF NOT FOUND THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_RETOUCH_CLAIM_WORKER_STALE' USING ERRCODE='40001';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id FOR UPDATE;
 project:=(r.config_snapshot->>'project_id')::uuid;
 IF t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
 OR r.id IS NULL OR r.kind IS DISTINCT FROM 'apply' OR r.status NOT IN('queued','running')
 OR NOT r.graph_sealed OR r.cancel_requested OR t.cancel_requested
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR coalesce(t.execution_profile->>'contracts_sha' NOT IN ('886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90','7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0'),true)
 OR jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes') IS DISTINCT FROM 'array'
 OR photo.v27_downstream_profile_ready(t.tenant_id,project,t.operation,t.operation_version,'cpu',t.execution_profile) IS DISTINCT FROM true
 THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_RETOUCH_CLAIM_STALE' USING ERRCODE='40001';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b JOIN photo.semantic_targets target
 ON target.tenant_id=b.tenant_id AND target.batch_id=b.id AND target.child_run_id=b.child_run_id
 WHERE b.tenant_id=t.tenant_id AND b.child_run_id=r.id AND b.project_id=project AND b.actor_id=r.created_by
 AND b.action='apply' AND target.state='queued' AND target.project_id=project
 AND t.id=photo.semantic_uuid_v5(r.id,'semantic.adopt_shown_retouch_region:'||target.id::text)
 AND (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 WHERE n->>'task_id'=t.id::text AND n->>'target_id'=target.id::text
 AND n->>'params_artifact_id'=a.id::text AND n->>'params_sha'=a.sha256)=1)
 OR (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id) NOT BETWEEN 3 AND 514
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
 AND (i.project_id IS DISTINCT FROM project OR a.id IS NULL OR a.sha256 IS DISTINCT FROM i.sha256
 OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL
 OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())))
 THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_RETOUCH_CLAIM_INPUTS_STALE' USING ERRCODE='40001';END IF;
 SELECT sum((execution_profile->'resource_estimate'->>'output_bytes')::numeric) INTO expected
 FROM photo.tasks WHERE tenant_id=t.tenant_id AND run_id=r.id;
 IF expected IS NULL OR expected NOT BETWEEN 1 AND 9007199254740991
 OR NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id
 AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved' AND q.principal_type='user'
 AND q.principal_id=r.created_by AND q.files=0 AND q.share_id IS NULL AND q.bytes=expected::bigint
 AND q.expires_at>clock_timestamp()) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_RETOUCH_CLAIM_QUOTA_STALE' USING ERRCODE='40001';END IF;
 IF r.status='queued' THEN
 UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id
 AND status='queued' AND NOT cancel_requested AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_RETOUCH_START_STALE' USING ERRCODE='40001';END IF;
 END IF;
 END IF;
 END IF;
 RETURN QUERY SELECT claimed.outcome,claimed.fence,claimed.lease_until,claimed.deadline,claimed.context;
END $$;
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_shown_retouch_task_live(p_task uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;expected numeric;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND THEN RETURN false;END IF;
 IF t.operation IS DISTINCT FROM 'semantic.adopt_shown_retouch_region' THEN RETURN true;END IF;
 -- The gateway session is not the worker DB principal. Bind the registered
 -- renderer to the task's lease worker; certificate/fence/principal checks
 -- remain in the existing gateway/heartbeat predecessor chain.
 PERFORM w.worker_id FROM photo.worker_registrations w
 WHERE w.tenant_id=t.tenant_id AND w.worker_id=t.worker_id AND w.enabled
 AND w.resource_classes ? 'cpu' AND w.backends ? 'cpu'
 AND (w.qualified_operations->'semantic.adopt_shown_retouch_region') ? 'cpu'
 AND w.v2_renderer_versions ? 'v2.2'
 AND photo.capacity_policy_worker_fresh(w) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'apply' OR r.status IS DISTINCT FROM 'running'
 OR NOT r.graph_sealed OR r.cancel_requested OR t.cancel_requested
 OR t.state IS DISTINCT FROM 'running' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR coalesce(t.execution_profile->>'contracts_sha' NOT IN ('886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90','7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0'),true)
 OR jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes') IS DISTINCT FROM 'array'
 THEN RETURN false;END IF;
 project:=(r.config_snapshot->>'project_id')::uuid;
 IF photo.v27_downstream_profile_ready(t.tenant_id,project,t.operation,t.operation_version,'cpu',t.execution_profile) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=t.tenant_id AND status='active')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b JOIN photo.semantic_targets target
 ON target.tenant_id=b.tenant_id AND target.batch_id=b.id AND target.child_run_id=b.child_run_id
 JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id
 JOIN photo.runs preview ON preview.tenant_id=intent.tenant_id
 AND preview.config_snapshot->'semantic_retouch_preview'->'params'->>'intent_id'=intent.id::text
 AND preview.config_snapshot->'semantic_retouch_preview'->'params'->>'target_id'=target.id::text
 JOIN photo.tasks producer ON producer.tenant_id=preview.tenant_id AND producer.run_id=preview.id
 AND producer.id::text=preview.v27_retouch_preview_receipt->>'task_id'
 WHERE b.tenant_id=t.tenant_id AND b.child_run_id=r.id AND b.project_id=project
 AND b.actor_id=r.created_by AND b.action='apply' AND target.project_id=project AND target.state='queued'
 AND preview.kind='preview' AND preview.graph_sealed AND NOT preview.cancel_requested
 AND preview.created_by=r.created_by AND producer.state='succeeded' AND NOT producer.cancel_requested
 AND producer.canonical_result_id::text=preview.v27_retouch_preview_receipt->>'result_id'
 AND producer.attempt::text=preview.v27_retouch_preview_receipt->>'attempt'
 AND producer.fencing_token::text=preview.v27_retouch_preview_receipt->>'fence'
 AND producer.operation='semantic.preview_retouch_region' AND producer.operation_version='1'
 AND producer.resource_class='cpu'
 AND photo.semantic_retouch_preview_bundle_ready(producer.id,producer.canonical_result_id,producer.attempt,producer.fencing_token) IS TRUE
 AND producer.id=photo.semantic_uuid_v5(preview.id,'semantic.preview_retouch_region:'||target.id::text)
 AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['presentation','working_image','pixels','numerical_evidence']) field
 WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=t.tenant_id AND input.task_id=t.id
 AND input.artifact_id::text=preview.v27_retouch_preview_receipt->field->>'artifact_id'
 AND input.sha256=preview.v27_retouch_preview_receipt->field->>'sha256'))
 AND photo.v2_editor_authorized(t.tenant_id,project,r.created_by,
 (preview.config_snapshot->'semantic_retouch_preview'->>'session_id')::uuid) IS TRUE
 AND t.id=photo.semantic_uuid_v5(r.id,'semantic.adopt_shown_retouch_region:'||target.id::text)
 AND (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 WHERE n->>'task_id'=t.id::text AND n->>'target_id'=target.id::text
 AND n->>'params_artifact_id'=a.id::text AND n->>'params_sha'=a.sha256)=1)
 THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id) NOT BETWEEN 3 AND 514
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND (i.project_id IS DISTINCT FROM project
 OR a.id IS NULL OR a.sha256 IS DISTINCT FROM i.sha256 OR a.state IS DISTINCT FROM 'ready'
 OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 SELECT sum((execution_profile->'resource_estimate'->>'output_bytes')::numeric) INTO expected
 FROM photo.tasks WHERE tenant_id=t.tenant_id AND run_id=r.id;
 RETURN coalesce(expected BETWEEN 1 AND 9007199254740991 AND EXISTS(
 SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id AND q.holder_type='run'
 AND q.holder_id=r.id AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by
 AND q.files=0 AND q.share_id IS NULL AND q.bytes=expected::bigint AND q.expires_at>clock_timestamp()),false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_retouch_task_live(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_retouch_task_live(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal,photo_semantic_admission;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
