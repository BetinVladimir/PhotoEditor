-- +goose Up
-- Keep reject-only review mutation inside the public safe revision range.
-- Preserve receipt-first locks, source authority, atomic events and narrow ACL.
-- Existing rows/receipts are not rewritten; forward-only.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.commit_semantic_rejections(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_key text,p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.semantic_intents%ROWTYPE; w photo.v2_workflows%ROWTYPE; r photo.runs%ROWTYPE;
 idem photo.idempotency_records%ROWTYPE; document photo.backend_documents%ROWTYPE;
 request_sha text; request jsonb; doc jsonb; owner uuid; remaining integer; n integer; cursor_value bigint; event_id uuid; response jsonb;
BEGIN
 IF p_request_raw IS NULL OR octet_length(p_request_raw) NOT BETWEEN 2 AND 65536
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9007199254740990 THEN
  RAISE EXCEPTION 'SEMANTIC_REJECTION_INVALID' USING ERRCODE='22023';END IF;
 request_sha:=encode(sha256(convert_to(p_request_raw,'UTF8')),'hex');
 request:=p_request_raw::jsonb;doc:=p_body::jsonb;
 SELECT * INTO i FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF i.id IS NULL OR NOT photo.v2_editor_authorized(p_tenant,i.project_id,p_actor,p_session) THEN
  RAISE EXCEPTION 'SEMANTIC_REJECTION_FORBIDDEN' USING ERRCODE='42501';END IF;
 -- Receipt first, before parent/source/target row locks.
 SELECT * INTO idem FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='SemanticIntent.Apply' AND key=p_key FOR UPDATE;
 IF idem.id IS NULL OR idem.request_hash IS DISTINCT FROM request_sha OR idem.response_status IS NOT NULL THEN
  RAISE EXCEPTION 'SEMANTIC_REJECTION_RECEIPT_CONFLICT' USING ERRCODE='40001';END IF;
 owner:=photo.lock_semantic_decision_batch(p_tenant,p_intent,p_actor,p_session,p_revision,p_request_raw,p_artifact,p_body,p_documents);
 -- This public commit remains strictly reject-only even though the shared gate
 -- also accepts approvals for the future approved-child transaction.
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(doc->'targets') d WHERE d->>'decision'<>'reject') THEN
  RAISE EXCEPTION 'SEMANTIC_REJECTION_TARGET_CONFLICT' USING ERRCODE='40001';END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=i.workflow_id;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id;
 n:=jsonb_array_length(doc->'targets');
 INSERT INTO photo.v2_review_batches(id,tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action)
 VALUES(owner,p_tenant,i.project_id,i.workflow_id,p_artifact,p_actor,'decide');
 UPDATE photo.semantic_targets SET state='rejected',batch_id=owner WHERE tenant_id=p_tenant AND intent_id=i.id
 AND id::text IN(SELECT d->>'target_id' FROM jsonb_array_elements(doc->'targets') d);
 GET DIAGNOSTICS remaining=ROW_COUNT;
 IF remaining<>n THEN RAISE EXCEPTION 'SEMANTIC_REJECTION_PARTIAL' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET revision=revision+1 WHERE tenant_id=p_tenant AND id=r.id AND revision=p_revision AND revision<9007199254740991 AND status='waiting_review' AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_REJECTION_STALE' USING ERRCODE='40001';END IF;
 SELECT count(*) INTO remaining FROM photo.semantic_targets WHERE tenant_id=p_tenant AND intent_id=i.id AND state='proposed';
 response:=jsonb_build_object('workflow_id',i.workflow_id,'revision',(p_revision+1)::text,'remaining',remaining,'child',NULL,'batch_id',owner);
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
-- +goose StatementEnd
ALTER FUNCTION photo.commit_semantic_rejections(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_semantic_rejections(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.commit_semantic_rejections(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
