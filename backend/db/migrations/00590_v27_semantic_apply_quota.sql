-- +goose Up
-- Private quota reservation from exact persisted draft profiles. The caller
-- must separately prove source dimensions and measured owner costs. Draft-only
-- exact replay never doubles accounting or renews an expired reservation.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_semantic_apply_quota(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_params jsonb,p_nodes jsonb) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant photo.tenants%ROWTYPE;usage photo.tenant_usage%ROWTYPE;q photo.quota_reservations%ROWTYPE;amount numeric;limit_bytes numeric;
BEGIN
 SELECT * INTO tenant FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF tenant.id IS NULL OR usage.id IS NULL THEN RAISE EXCEPTION 'SEMANTIC_APPLY_QUOTA_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_PROJECT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child AND kind='apply' AND status='draft'
 AND NOT graph_sealed AND NOT cancel_requested AND created_by=p_actor AND config_snapshot->>'project_id'=p_project::text
 AND config_snapshot ? 'approved_semantic' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_QUOTA_SCOPE_INVALID' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child ORDER BY id FOR UPDATE;
 IF NOT photo.semantic_approved_exposure_nodes_ready(p_tenant,p_project,p_child,p_params,p_nodes)
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child)<>jsonb_array_length(p_nodes)
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(p_nodes) n WHERE NOT EXISTS(SELECT 1 FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.run_id=p_child AND t.id::text=n->>'id' AND t.state='blocked' AND t.attempt=0 AND NOT t.cancel_requested
 AND t.node_key=n->>'node_key' AND t.operation=n->>'operation' AND t.operation_version=n->>'operation_version'
 AND t.resource_class=n->>'resource_class' AND t.params_artifact_id::text=n->>'params_artifact_id'
 AND t.input_digest=n->>'input_digest' AND t.execution_profile=n->'execution_profile')) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_QUOTA_PROFILE_INVALID' USING ERRCODE='23514';END IF;
 SELECT sum((n->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric) INTO amount FROM jsonb_array_elements(p_nodes) n;
 limit_bytes:=(tenant.quotas->>'storage_bytes')::numeric;
 IF amount IS NULL OR amount NOT BETWEEN 1 AND 9007199254740991 OR limit_bytes IS NULL OR limit_bytes<0 THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_QUOTA_INVALID' USING ERRCODE='23514';END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_child FOR UPDATE;
 IF q.id IS NOT NULL THEN
  IF q.principal_type<>'user' OR q.principal_id IS DISTINCT FROM p_actor OR q.share_id IS NOT NULL OR q.state<>'reserved'
  OR q.bytes<>amount OR q.files<>0 OR q.expires_at<=clock_timestamp()
  OR usage.storage_bytes::numeric+usage.reserved_bytes>least(limit_bytes,9007199254740991) THEN
  RAISE EXCEPTION 'SEMANTIC_APPLY_QUOTA_CONFLICT' USING ERRCODE='23514';END IF;
  RETURN amount::bigint;
 END IF;
 IF usage.storage_bytes::numeric+usage.reserved_bytes+amount>least(limit_bytes,9007199254740991) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_QUOTA_EXCEEDED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',p_actor,'run',p_child,amount::bigint,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+amount::bigint WHERE tenant_id=p_tenant;
 RETURN amount::bigint;
END $$;
ALTER FUNCTION photo.reserve_semantic_apply_quota(uuid,uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_semantic_apply_quota(uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
-- Shell acquires tenant/usage before source/run/task/profile locks and reserves
-- output budget in the same SQL statement. Any refusal rolls back the shell.
ALTER FUNCTION photo.create_semantic_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb,jsonb) RENAME TO create_semantic_apply_draft_pre_quota;
REVOKE ALL ON FUNCTION photo.create_semantic_apply_draft_pre_quota(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb,jsonb) FROM PUBLIC;
CREATE FUNCTION photo.create_semantic_apply_draft_shell(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb,p_child uuid,p_params jsonb,p_nodes jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE batch uuid;project uuid;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_TENANT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_QUOTA_UNAVAILABLE' USING ERRCODE='23514';END IF;
 batch:=photo.create_semantic_apply_draft_pre_quota(p_tenant,p_intent,p_actor,p_session,p_revision,p_request_raw,p_artifact,p_body,p_documents,p_child,p_params,p_nodes);
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 PERFORM photo.reserve_semantic_apply_quota(p_tenant,project,p_actor,p_child,p_params,p_nodes);
 RETURN batch;
END $$;
ALTER FUNCTION photo.create_semantic_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_semantic_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
