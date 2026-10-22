-- +goose Up
-- Exact draft-only reservation. Caller retains all source/decision locks and
-- completes seal/activation/receipt atomically. Replay never extends expiry.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_semantic_shown_apply_quota(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant photo.tenants%ROWTYPE;usage photo.tenant_usage%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 amount numeric;limit_bytes numeric;row jsonb;
BEGIN
 SELECT * INTO tenant FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_PROJECT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_SCOPE_INVALID' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child ORDER BY id FOR UPDATE;
 IF photo.semantic_shown_apply_draft_ready(p_tenant,p_project,p_actor,p_child,p_tasks) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_DRAFT_INVALID' USING ERRCODE='23514';END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
 IF photo.semantic_shown_exposure_profile_bound((row->>'params_body')::jsonb,row->'node'->'execution_profile',
 (row->'documents'->>'image')::jsonb) IS DISTINCT FROM true
 OR photo.v27_downstream_profile_ready(p_tenant,p_project,'semantic.adopt_shown_exposure','1','cpu',row->'node'->'execution_profile') IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_PROFILE_INVALID' USING ERRCODE='23514';END IF;
 END LOOP;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_UNAVAILABLE' USING ERRCODE='23514';END IF;
 SELECT sum((x.value->'node'->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric)
 INTO amount FROM jsonb_array_elements(p_tasks) AS x(value);
 limit_bytes:=(tenant.quotas->>'storage_bytes')::numeric;
 IF amount IS NULL OR amount NOT BETWEEN 1 AND 9007199254740991 OR amount<>trunc(amount)
 OR limit_bytes IS NULL OR limit_bytes<0 OR limit_bytes<>trunc(limit_bytes)
 OR usage.storage_bytes IS NULL OR usage.reserved_bytes IS NULL OR usage.storage_bytes<0 OR usage.reserved_bytes<0 THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_INVALID' USING ERRCODE='23514';END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_child FOR UPDATE;
 IF FOUND THEN
 IF q.principal_type IS DISTINCT FROM 'user' OR q.principal_id IS DISTINCT FROM p_actor OR q.share_id IS NOT NULL
 OR q.state IS DISTINCT FROM 'reserved' OR q.bytes IS DISTINCT FROM amount::bigint OR q.files IS DISTINCT FROM 0
 OR q.expires_at IS NULL OR q.expires_at<=clock_timestamp()
 OR usage.storage_bytes::numeric+usage.reserved_bytes>least(limit_bytes,9007199254740991) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_CONFLICT' USING ERRCODE='23514';END IF;
 RETURN amount::bigint;
 END IF;
 IF usage.storage_bytes::numeric+usage.reserved_bytes+amount>least(limit_bytes,9007199254740991) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_EXCEEDED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',p_actor,'run',p_child,amount::bigint,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+amount::bigint WHERE tenant_id=p_tenant;
 RETURN amount::bigint;
END $$;
ALTER FUNCTION photo.reserve_semantic_shown_apply_quota(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_semantic_shown_apply_quota(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
