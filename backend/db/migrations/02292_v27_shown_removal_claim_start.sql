-- +goose Up
-- Removal copy claim/start is atomic; retain the complete existing claim chain.
-- Claim/start is atomic. Full raw Params/receipt proof repeats in worker admission
-- before any image IO; this gate checks persisted scope, inputs and live capacity.
-- +goose StatementBegin
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) RENAME TO claim_task_before_shown_removal;
REVOKE ALL ON FUNCTION photo.claim_task_before_shown_removal(uuid,integer,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
CREATE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE claimed record;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;expected numeric;
BEGIN
 SELECT * INTO claimed FROM photo.claim_task_before_shown_removal(p_task,p_attempt,p_worker,p_command);
 IF claimed.outcome='claimed' THEN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.operation='semantic.adopt_shown_removal' THEN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id FOR UPDATE;
 project:=(r.config_snapshot->>'project_id')::uuid;
 IF t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
 OR r.id IS NULL OR r.kind IS DISTINCT FROM 'apply' OR r.status NOT IN('queued','running')
 OR NOT r.graph_sealed OR r.cancel_requested OR t.cancel_requested
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR t.execution_profile->>'contracts_sha' IS DISTINCT FROM '13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5'
 OR jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes') IS DISTINCT FROM 'array'
 OR photo.v27_downstream_profile_ready(t.tenant_id,project,t.operation,t.operation_version,'cpu',t.execution_profile) IS DISTINCT FROM true
 THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_REMOVAL_CLAIM_STALE' USING ERRCODE='40001';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b JOIN photo.semantic_targets target
 ON target.tenant_id=b.tenant_id AND target.batch_id=b.id AND target.child_run_id=b.child_run_id
 WHERE b.tenant_id=t.tenant_id AND b.child_run_id=r.id AND b.project_id=project AND b.actor_id=r.created_by
 AND b.action='apply' AND target.state='queued' AND target.project_id=project
 AND t.id=photo.semantic_uuid_v5(r.id,'semantic.adopt_shown_removal:'||target.id::text)
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
 THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_REMOVAL_CLAIM_INPUTS_STALE' USING ERRCODE='40001';END IF;
 SELECT sum((execution_profile->'resource_estimate'->>'output_bytes')::numeric) INTO expected
 FROM photo.tasks WHERE tenant_id=t.tenant_id AND run_id=r.id;
 IF expected IS NULL OR expected NOT BETWEEN 1 AND 9007199254740991
 OR NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id
 AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved' AND q.principal_type='user'
 AND q.principal_id=r.created_by AND q.files=0 AND q.share_id IS NULL AND q.bytes=expected::bigint
 AND q.expires_at>clock_timestamp()) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_REMOVAL_CLAIM_QUOTA_STALE' USING ERRCODE='40001';END IF;
 IF r.status='queued' THEN
 UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id
 AND status='queued' AND NOT cancel_requested AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_REMOVAL_START_STALE' USING ERRCODE='40001';END IF;
 END IF;
 END IF;
 END IF;
 RETURN QUERY SELECT claimed.outcome,claimed.fence,claimed.lease_until,claimed.deadline,claimed.context;
END $$;
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
