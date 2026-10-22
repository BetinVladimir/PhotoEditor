-- +goose Up
-- Preserve the installed claim chain, including signer and series device gates.
-- Preview checks and run start commit atomically with successful lease claim.
-- +goose StatementBegin
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) RENAME TO claim_task_before_retouch_preview;
REVOKE ALL ON FUNCTION photo.claim_task_before_retouch_preview(uuid,integer,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
CREATE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE claimed record;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;
BEGIN
 SELECT * INTO claimed FROM photo.claim_task_before_retouch_preview(p_task,p_attempt,p_worker,p_command);
 IF claimed.outcome='claimed' THEN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.operation='semantic.preview_retouch_region' THEN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id FOR UPDATE;
 SELECT i.project_id INTO project FROM photo.semantic_intents i WHERE i.tenant_id=t.tenant_id
 AND i.id::text=r.config_snapshot->'semantic_retouch_preview'->'params'->>'intent_id';
 IF t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
 OR r.id IS NULL OR r.kind<>'preview' OR r.status NOT IN('queued','running') OR NOT r.graph_sealed OR r.cancel_requested
 OR photo.semantic_retouch_preview_closure_ready(t.id) IS DISTINCT FROM true
 OR photo.semantic_retouch_preview_run_current(t.tenant_id,r.id) IS DISTINCT FROM true
 OR photo.v27_downstream_profile_ready(t.tenant_id,project,'semantic.preview_retouch_region','1','cpu',
 r.v27_preapply_preview_profile) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id
 AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved' AND q.principal_type='user'
 AND q.principal_id=r.created_by AND q.files=0 AND q.share_id IS NULL
 AND q.bytes=(r.v27_preapply_preview_profile->'resource_estimate'->>'output_bytes')::bigint
 AND q.expires_at>clock_timestamp()) THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_CLAIM_STALE' USING ERRCODE='40001';END IF;
 IF r.status='queued' THEN
 UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id
 AND status='queued' AND NOT cancel_requested AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_PREVIEW_START_STALE' USING ERRCODE='40001';END IF;
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
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
