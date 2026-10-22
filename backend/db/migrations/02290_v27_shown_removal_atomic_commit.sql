-- +goose Up
-- Removal-only atomic review commit: draft/quota/inputs/activation/receipt/events.
-- Private transaction component only; caller must commit parent/receipt/events atomically.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_semantic_shown_removal_apply_child(p_tenant uuid,p_project uuid,p_parent uuid,p_child uuid,p_actor uuid,p_tasks jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE q photo.quota_reservations%ROWTYPE; expected numeric; changed integer;row jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent AND kind='v2'
 AND status='waiting_review' AND NOT cancel_requested FOR UPDATE;
 IF NOT FOUND OR p_parent=p_child THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child AND kind='apply'
 AND status='draft' AND NOT graph_sealed AND NOT cancel_requested AND created_by=p_actor
 AND config_snapshot->>'project_id'=p_project::text FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child ORDER BY id FOR UPDATE;
 IF photo.semantic_shown_removal_apply_sealed_inputs_ready(p_tenant,p_project,p_actor,p_child,p_tasks) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b JOIN photo.v2_workflows w
 ON w.tenant_id=b.tenant_id AND w.id=b.workflow_id AND w.run_id=p_parent
 WHERE b.tenant_id=p_tenant AND b.project_id=p_project AND b.child_run_id=p_child
 AND b.actor_id=p_actor AND b.action='apply') THEN RETURN false;END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
 IF photo.v27_downstream_profile_ready(p_tenant,p_project,'semantic.adopt_shown_removal','1','cpu',row->'node'->'execution_profile') IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 SELECT sum((x.value->'node'->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric)
 INTO expected FROM jsonb_array_elements(p_tasks) AS x(value);
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_child FOR UPDATE;
 IF expected IS NULL OR expected NOT BETWEEN 1 AND 9007199254740991 OR q.id IS NULL
 OR q.state IS DISTINCT FROM 'reserved' OR q.bytes IS DISTINCT FROM expected::bigint OR q.files IS DISTINCT FROM 0 OR q.principal_type IS DISTINCT FROM 'user'
 OR q.principal_id IS DISTINCT FROM p_actor OR q.share_id IS NOT NULL OR q.expires_at IS NULL OR q.expires_at<=clock_timestamp() THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready' WHERE tenant_id=p_tenant AND run_id=p_child AND state='blocked' AND attempt=0 AND NOT cancel_requested;
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>jsonb_array_length(p_tasks) THEN RAISE EXCEPTION 'SEMANTIC_APPLY_ACTIVATION_PARTIAL' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET graph_sealed=true,status='queued' WHERE tenant_id=p_tenant AND id=p_child AND status='draft' AND NOT graph_sealed AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_ACTIVATION_STALE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.activate_semantic_shown_removal_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_semantic_shown_removal_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private completion after source-byte recheck/draft/quota/seal in same TX.
-- Caller must hold the receipt before all source/draft work; no runtime grants.
-- +goose StatementBegin
CREATE FUNCTION photo.finish_semantic_shown_removal_apply_review(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_key text,p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_child uuid,p_tasks jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.semantic_intents%ROWTYPE; w photo.v2_workflows%ROWTYPE; r photo.runs%ROWTYPE;
 idem photo.idempotency_records%ROWTYPE; owner uuid; request_sha text; doc jsonb; remaining integer; cursor_value bigint; event_id uuid; response jsonb;
BEGIN
 IF p_request_raw IS NULL OR octet_length(p_request_raw) NOT BETWEEN 2 AND 65536
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9007199254740990 THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_FINISH_INVALID' USING ERRCODE='22023';END IF;
 request_sha:=encode(sha256(convert_to(p_request_raw,'UTF8')),'hex');doc:=p_body::jsonb;
 SELECT * INTO i FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF i.id IS NULL OR NOT photo.v2_editor_authorized(p_tenant,i.project_id,p_actor,p_session) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_FINISH_FORBIDDEN' USING ERRCODE='42501';END IF;
 SELECT * INTO idem FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='SemanticIntent.Apply' AND key=p_key FOR UPDATE;
 IF idem.id IS NULL OR idem.request_hash IS DISTINCT FROM request_sha OR idem.response_status IS NOT NULL THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_RECEIPT_CONFLICT' USING ERRCODE='40001';END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=i.workflow_id;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR UPDATE;
 owner:=photo.semantic_decision_document_owner(p_tenant,r.id,p_actor,request_sha);
 IF r.id IS NULL OR r.revision<>p_revision OR r.status<>'waiting_review' OR r.cancel_requested
 OR (p_request_raw::jsonb)->>'run_id' IS DISTINCT FROM r.id::text
 OR doc->>'run_id' IS DISTINCT FROM r.id::text OR doc->>'expected_revision' IS DISTINCT FROM p_revision::text
 OR jsonb_typeof(doc->'targets') IS DISTINCT FROM 'array' THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_FINISH_STALE' USING ERRCODE='40001';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
 ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.object_version=d.object_version
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE d.tenant_id=p_tenant AND d.run_id=r.id AND d.owner_id=owner AND d.purpose='review_selection'
 AND d.state='ready' AND d.artifact_id=p_artifact AND d.sha256=encode(sha256(convert_to(p_body,'UTF8')),'hex') AND d.bytes=octet_length(p_body))
 OR NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b WHERE b.tenant_id=p_tenant AND b.id=owner
 AND b.workflow_id=w.id AND b.project_id=i.project_id AND b.actor_id=p_actor AND b.action='apply'
 AND b.child_run_id=p_child AND b.decisions_artifact_id=p_artifact)
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(doc->'targets') d WHERE d->>'decision'='approve')
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(doc->'targets') d LEFT JOIN photo.semantic_targets t
 ON t.tenant_id=p_tenant AND t.intent_id=p_intent AND t.id::text=d->>'target_id'
 WHERE t.id IS NULL OR t.batch_id IS DISTINCT FROM owner OR d->>'decision' NOT IN('approve','reject')
 OR t.state IS DISTINCT FROM CASE WHEN d->>'decision'='approve' THEN 'queued' ELSE 'rejected' END
 OR t.child_run_id IS DISTINCT FROM CASE WHEN d->>'decision'='approve' THEN p_child ELSE NULL END)
 THEN RAISE EXCEPTION 'SEMANTIC_APPLY_FINISH_BINDING_INVALID' USING ERRCODE='23514';END IF;
 IF photo.semantic_shown_removal_batch_nodes_bound(p_child,doc->'targets',p_tasks) IS DISTINCT FROM true
 OR photo.activate_semantic_shown_removal_apply_child(p_tenant,i.project_id,r.id,p_child,p_actor,p_tasks) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_ACTIVATION_DENIED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET revision=revision+1 WHERE tenant_id=p_tenant AND id=r.id AND revision=p_revision AND status='waiting_review' AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_FINISH_STALE' USING ERRCODE='40001';END IF;
 SELECT count(*) INTO remaining FROM photo.semantic_targets WHERE tenant_id=p_tenant AND intent_id=i.id AND state='proposed';
 response:=jsonb_build_object('workflow_id',i.workflow_id,'revision',(p_revision+1)::text,'remaining',remaining,
 'child',jsonb_build_object('run_id',p_child,'state','queued'),'batch_id',owner);
 UPDATE photo.idempotency_records SET response_status=200,response_body=response,resource_id=owner WHERE tenant_id=p_tenant AND id=idem.id;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(p_tenant,'user',p_actor,'semantic.review.batch','semantic_intent',i.id,idem.id,response);
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF cursor_value IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=gen_random_uuid();
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,cursor_value,r.id,p_actor,'principal','v27.review.batch',response);
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v27.review.batch',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v27.review.batch','tenant_id',p_tenant,'run_id',r.id,'result',response));
 RETURN response;
END $$;
ALTER FUNCTION photo.finish_semantic_shown_removal_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_semantic_shown_removal_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Only complete mutation entry point: receipt lock -> source/decision/preview
-- locks -> draft/quota -> registration/seal -> activation/parent/receipt/events.
-- Any refusal raises and rolls back the whole statement. Completed idempotency
-- replay is handled by the API before entering this new-mutation function.
-- +goose StatementBegin
CREATE FUNCTION photo.commit_semantic_shown_removal_apply_review(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
 p_key text,p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb,p_child uuid,p_tasks jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE;batch uuid;project uuid;sealed integer;response jsonb;
BEGIN
 IF p_request_raw IS NULL OR octet_length(p_request_raw) NOT BETWEEN 2 AND 65536
 OR p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9007199254740990 THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_COMMIT_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO idem FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='SemanticIntent.Apply' AND key=p_key FOR UPDATE;
 IF NOT FOUND OR idem.request_hash IS DISTINCT FROM encode(sha256(convert_to(p_request_raw,'UTF8')),'hex')
 OR idem.response_status IS NOT NULL THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_RECEIPT_CONFLICT' USING ERRCODE='40001';END IF;
 batch:=photo.create_semantic_shown_removal_apply_reserved_draft(p_tenant,p_intent,p_actor,p_session,p_revision,
 p_request_raw,p_artifact,p_body,p_documents,p_child,p_tasks);
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_INTENT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 sealed:=photo.seal_semantic_shown_removal_apply_inputs(p_tenant,project,p_actor,p_child,p_tasks);
 IF sealed IS DISTINCT FROM jsonb_array_length(p_tasks) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_SEAL_INCOMPLETE' USING ERRCODE='23514';END IF;
 response:=photo.finish_semantic_shown_removal_apply_review(p_tenant,p_intent,p_actor,p_session,p_key,p_revision,
 p_request_raw,p_artifact,p_body,p_child,p_tasks);
 IF response->>'batch_id' IS DISTINCT FROM batch::text OR response->'child'->>'run_id' IS DISTINCT FROM p_child::text THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_RECEIPT_INVALID' USING ERRCODE='23514';END IF;
 RETURN response;
END $$;
ALTER FUNCTION photo.commit_semantic_shown_removal_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_semantic_shown_removal_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.commit_semantic_shown_removal_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb)
 TO photo_semantic_admission;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
