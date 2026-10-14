-- +goose Up
-- Private transaction component only: caller holds receipt before entry and
-- must complete quota, full source inventory, seal, activation and receipt/event
-- commit in the SAME transaction, or roll back all shell/target changes.
-- No runtime-role grant; this does not queue tasks or emit dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.create_semantic_apply_draft_shell(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb,p_child uuid,p_params jsonb,p_nodes jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent photo.semantic_intents%ROWTYPE;parent uuid;batch uuid;config_raw text;bindings text;node jsonb;decisions jsonb;changed integer;
BEGIN
 SELECT * INTO intent FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF intent.id IS NULL THEN RAISE EXCEPTION 'SEMANTIC_APPLY_INTENT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 SELECT run_id INTO parent FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=intent.workflow_id;
 batch:=photo.lock_semantic_decision_batch(p_tenant,p_intent,p_actor,p_session,p_revision,p_request_raw,p_artifact,p_body,p_documents);
 decisions:=(p_body::jsonb)->'targets';
 IF NOT photo.semantic_approved_child_params_ready(p_tenant,parent,p_intent,p_child,decisions,p_params,p_documents)
 OR NOT photo.semantic_approved_exposure_nodes_ready(p_tenant,intent.project_id,p_child,p_params,p_nodes) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_DRAFT_INVALID' USING ERRCODE='23514';END IF;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE id=p_child)
 OR EXISTS(SELECT 1 FROM photo.v2_review_batches WHERE id=batch) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_DRAFT_CONFLICT' USING ERRCODE='40001';END IF;
 -- Closed UUID/SHA fields permit exact JCS without jsonb whitespace or key order.
 SELECT '['||string_agg('{"params_artifact_id":"'||(p->>'artifact_id')::uuid::text||'","params_sha":"'||(p->>'sha256')||'","target_id":"'||(p->>'target_id')::uuid::text||'","task_id":"'||(p->>'task_id')::uuid::text||'"}',',' ORDER BY (p->>'task_id')::uuid)||']'
 INTO bindings FROM jsonb_array_elements(p_params) p;
 config_raw:='{"approved_semantic":{"nodes":'||bindings||'},"project_id":"'||intent.project_id::text||'"}';
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy)
 VALUES(p_child,p_tenant,'apply','draft',intent.snapshot_id,config_raw::jsonb,encode(sha256(convert_to(config_raw,'UTF8')),'hex'),p_actor,'fail_fast');
 INSERT INTO photo.v2_review_batches(id,tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action,child_run_id)
 VALUES(batch,p_tenant,intent.project_id,intent.workflow_id,p_artifact,p_actor,'apply',p_child);
 FOR node IN SELECT value FROM jsonb_array_elements(p_nodes) ORDER BY value->>'id' LOOP
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES((node->>'id')::uuid,p_tenant,p_child,node->>'node_key',node->>'operation',node->>'operation_version',node->>'resource_class','blocked','[]',
 (node->>'params_artifact_id')::uuid,node->>'input_digest',node->'execution_profile');
 END LOOP;
 UPDATE photo.semantic_targets t SET state=CASE WHEN d->>'decision'='approve' THEN 'queued' ELSE 'rejected' END,
 batch_id=batch,child_run_id=CASE WHEN d->>'decision'='approve' THEN p_child ELSE NULL END
 FROM jsonb_array_elements(decisions) d WHERE t.tenant_id=p_tenant AND t.intent_id=p_intent AND t.id::text=d->>'target_id' AND t.state='proposed';
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>jsonb_array_length(decisions) THEN RAISE EXCEPTION 'SEMANTIC_APPLY_DRAFT_PARTIAL' USING ERRCODE='23514';END IF;
 RETURN batch;
END $$;
ALTER FUNCTION photo.create_semantic_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_semantic_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
