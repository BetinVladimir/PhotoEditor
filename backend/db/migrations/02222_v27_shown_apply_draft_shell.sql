-- +goose Up
-- Private transaction component. Caller must hold idempotency receipt first and
-- complete quota, relational input registration, seal, activation and receipt/
-- parent/events in the SAME transaction, or roll back all shell/target changes.
-- No runtime grant and no dispatch: every task remains blocked.
-- +goose StatementBegin
CREATE FUNCTION photo.create_semantic_shown_apply_draft_shell(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
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
 batch:=photo.lock_semantic_shown_decision_batch(p_tenant,p_intent,p_actor,p_session,p_revision,
 p_request_raw,p_artifact,p_body,p_documents);
 decisions:=(p_body::jsonb)->'targets';
 IF photo.semantic_shown_batch_nodes_bound(p_child,decisions,p_tasks) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_BATCH_INVALID' USING ERRCODE='23514';END IF;
 -- Validate the complete batch before the first shell/target mutation.
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) ORDER BY value->>'target_id' LOOP
 IF photo.semantic_shown_exposure_admission_documents_ready(p_tenant,p_intent,p_actor,p_session,p_child,
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
ALTER FUNCTION photo.create_semantic_shown_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_semantic_shown_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
