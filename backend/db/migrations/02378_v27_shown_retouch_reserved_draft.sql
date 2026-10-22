-- +goose Up
-- Private retouch Apply shell + exact storage reservation; never dispatches.
-- Private transaction component. Caller must hold idempotency receipt first and
-- complete quota, relational input registration, seal, activation and receipt/
-- parent/events in the SAME transaction, or roll back all shell/target changes.
-- No runtime grant and no dispatch: every task remains blocked.
-- +goose StatementBegin
CREATE FUNCTION photo.create_semantic_shown_retouch_apply_draft_shell(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
 p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb,p_child uuid,p_tasks jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.semantic_intents%ROWTYPE;parent uuid;batch uuid;decisions jsonb;row jsonb;node jsonb;bindings text;config_raw text;changed integer;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_TENANT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 SELECT * INTO i FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_INTENT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 SELECT run_id INTO parent FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=i.workflow_id;
 IF NOT FOUND OR p_child IS NULL OR p_child=parent THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_CHILD_INVALID' USING ERRCODE='22023';END IF;
 batch:=photo.lock_semantic_shown_retouch_decision_batch(p_tenant,p_intent,p_actor,p_session,p_revision,
 p_request_raw,p_artifact,p_body,p_documents);
 decisions:=(p_body::jsonb)->'targets';
 IF photo.semantic_shown_retouch_batch_nodes_bound(p_child,decisions,p_tasks) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_BATCH_INVALID' USING ERRCODE='23514';END IF;
 -- Validate the complete batch before the first shell/target mutation.
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) ORDER BY value->>'target_id' LOOP
 IF photo.semantic_shown_retouch_admission_documents_ready(p_tenant,p_intent,p_actor,p_session,p_child,
 (row->>'target_id')::uuid,parent,p_revision,
 (SELECT (d->>'target_revision')::bigint FROM jsonb_array_elements(decisions) d WHERE d->>'target_id'=row->>'target_id'),
 row->>'params_body',row->>'params_sha',row->'documents',row->'node',row->'transport',row->'inventory') IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_INPUTS_UNAVAILABLE' USING ERRCODE='23514';END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE id=p_child) OR EXISTS(SELECT 1 FROM photo.v2_review_batches WHERE id=batch) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_DRAFT_CONFLICT' USING ERRCODE='40001';END IF;
 SELECT '['||string_agg('{"params_artifact_id":"'||(t->>'params_artifact_id')::uuid::text||'","params_sha":"'||(t->>'params_sha')||'","target_id":"'||(t->>'target_id')::uuid::text||'","task_id":"'||(t->'node'->>'id')::uuid::text||'"}',',' ORDER BY (t->'node'->>'id')::uuid)||']'
 INTO bindings FROM jsonb_array_elements(p_tasks) t;
 config_raw:='{"project_id":"'||i.project_id::text||'","shown_semantic":{"nodes":'||bindings||'}}';
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy)
 VALUES(p_child,p_tenant,'apply','draft',i.snapshot_id,config_raw::jsonb,encode(sha256(convert_to(config_raw,'UTF8')),'hex'),p_actor,'fail_fast');
 INSERT INTO photo.v2_review_batches(id,tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action,child_run_id)
 VALUES(batch,p_tenant,i.project_id,i.workflow_id,p_artifact,p_actor,'apply',p_child);
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) ORDER BY value->'node'->>'id' LOOP
 node:=row->'node';
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES((node->>'id')::uuid,p_tenant,p_child,node->>'node_key',node->>'operation',node->>'operation_version',node->>'resource_class','blocked','[]',
 (node->>'params_artifact_id')::uuid,node->>'input_digest',node->'execution_profile');
 END LOOP;
 UPDATE photo.semantic_targets t SET state=CASE WHEN d->>'decision'='approve' THEN 'queued' ELSE 'rejected' END,
 batch_id=batch,child_run_id=CASE WHEN d->>'decision'='approve' THEN p_child ELSE NULL END
 FROM jsonb_array_elements(decisions) d WHERE t.tenant_id=p_tenant AND t.intent_id=p_intent
 AND t.id::text=d->>'target_id' AND t.state='proposed';
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>jsonb_array_length(decisions) THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_DRAFT_PARTIAL' USING ERRCODE='23514';END IF;
 RETURN batch;
END $$;
ALTER FUNCTION photo.create_semantic_shown_retouch_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_semantic_shown_retouch_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Persisted draft equality gate for quota/seal. No activation or lease authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_retouch_apply_draft_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;row jsonb;node jsonb;t photo.tasks%ROWTYPE;bindings text;config_raw text;n integer;
BEGIN
 IF jsonb_typeof(p_tasks) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 n:=jsonb_array_length(p_tasks);
 IF n NOT BETWEEN 1 AND 100 OR n<>(SELECT count(DISTINCT x->'node'->>'id') FROM jsonb_array_elements(p_tasks) x) THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child;
 IF NOT FOUND OR r.kind IS DISTINCT FROM 'apply' OR r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR r.cancel_requested
 OR r.created_by IS DISTINCT FROM p_actor OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child)<>n THEN RETURN false;END IF;
 SELECT '['||string_agg('{"params_artifact_id":"'||(x->>'params_artifact_id')::uuid::text||'","params_sha":"'||(x->>'params_sha')||'","target_id":"'||(x->>'target_id')::uuid::text||'","task_id":"'||(x->'node'->>'id')::uuid::text||'"}',',' ORDER BY (x->'node'->>'id')::uuid)||']'
 INTO bindings FROM jsonb_array_elements(p_tasks) x;
 config_raw:='{"project_id":"'||p_project::text||'","shown_semantic":{"nodes":'||bindings||'}}';
 IF r.config_snapshot IS DISTINCT FROM config_raw::jsonb
 OR r.config_hash IS DISTINCT FROM encode(sha256(convert_to(config_raw,'UTF8')),'hex') THEN RETURN false;END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
 node:=row->'node';
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child AND id=(node->>'id')::uuid;
 IF NOT FOUND OR t.state IS DISTINCT FROM 'blocked' OR t.attempt<>0 OR t.cancel_requested
 OR t.operation IS DISTINCT FROM 'semantic.adopt_shown_retouch_region' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.node_key IS DISTINCT FROM node->>'node_key'
 OR t.params_artifact_id::text IS DISTINCT FROM row->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM node->>'input_digest'
 OR t.execution_profile IS DISTINCT FROM node->'execution_profile'
 OR photo.semantic_shown_retouch_transport_ready(p_child,(row->>'params_body')::jsonb,node,row->'transport',row->'inventory') IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.semantic_targets target JOIN photo.v2_review_batches batch
 ON batch.tenant_id=target.tenant_id AND batch.id=target.batch_id
 WHERE target.tenant_id=p_tenant AND target.id::text=row->>'target_id' AND target.state='queued'
 AND target.child_run_id=p_child AND batch.child_run_id=p_child AND batch.project_id=p_project
 AND batch.actor_id=p_actor AND batch.action='apply') THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_retouch_apply_draft_ready(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_retouch_apply_draft_ready(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Exact draft-only reservation. Caller retains all source/decision locks and
-- completes seal/activation/receipt atomically. Replay never extends expiry.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_semantic_shown_retouch_apply_quota(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS bigint
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
 IF photo.semantic_shown_retouch_apply_draft_ready(p_tenant,p_project,p_actor,p_child,p_tasks) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_DRAFT_INVALID' USING ERRCODE='23514';END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
 IF photo.semantic_shown_retouch_profile_bound((row->>'params_body')::jsonb,row->'node'->'execution_profile',
 (row->'documents'->>'image')::jsonb) IS DISTINCT FROM true
 OR photo.v27_downstream_profile_ready(p_tenant,p_project,'semantic.adopt_shown_retouch_region','1','cpu',row->'node'->'execution_profile') IS DISTINCT FROM true THEN
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
ALTER FUNCTION photo.reserve_semantic_shown_retouch_apply_quota(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_semantic_shown_retouch_apply_quota(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Compose shell and quota in one statement, so quota refusal rolls back all
-- draft and target mutations. Caller must still register/seal/activate and finish
-- parent/idempotency/events in the same transaction. Private; no dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.create_semantic_shown_retouch_apply_reserved_draft(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
 p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb,p_child uuid,p_tasks jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE batch uuid;project uuid;reserved bigint;
BEGIN
 batch:=photo.create_semantic_shown_retouch_apply_draft_shell(p_tenant,p_intent,p_actor,p_session,p_revision,
 p_request_raw,p_artifact,p_body,p_documents,p_child,p_tasks);
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_INTENT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 reserved:=photo.reserve_semantic_shown_retouch_apply_quota(p_tenant,project,p_actor,p_child,p_tasks);
 IF reserved IS NULL OR reserved<1 THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_UNAVAILABLE' USING ERRCODE='23514';END IF;
 RETURN batch;
END $$;
ALTER FUNCTION photo.create_semantic_shown_retouch_apply_reserved_draft(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_semantic_shown_retouch_apply_reserved_draft(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
