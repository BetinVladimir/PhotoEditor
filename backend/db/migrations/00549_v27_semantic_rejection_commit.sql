-- +goose Up
-- Rejection-only batch: no child Apply admission or GPU reservation.
-- +goose StatementBegin
CREATE FUNCTION photo.commit_semantic_rejections(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_key text,p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.semantic_intents%ROWTYPE; w photo.v2_workflows%ROWTYPE; r photo.runs%ROWTYPE;
 idem photo.idempotency_records%ROWTYPE; document photo.backend_documents%ROWTYPE;
 request_sha text; request jsonb; doc jsonb; owner uuid; remaining integer; n integer; cursor_value bigint; event_id uuid; response jsonb;
BEGIN
 IF p_request_raw IS NULL OR octet_length(p_request_raw) NOT BETWEEN 2 AND 65536
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9223372036854775806 THEN
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
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=i.workflow_id;
 IF photo.initialize_semantic_review(p_tenant,i.workflow_id,p_actor,p_session,p_revision,p_documents) IS DISTINCT FROM i.id THEN
  RAISE EXCEPTION 'SEMANTIC_REJECTION_INTENT_CONFLICT' USING ERRCODE='23514';END IF;
 PERFORM * FROM photo.lock_semantic_decision_targets(p_tenant,i.workflow_id,p_actor,p_session,p_revision);
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id;
 owner:=photo.semantic_decision_document_owner(p_tenant,r.id,p_actor,request_sha);
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact FOR SHARE;
 SELECT * INTO document FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=r.id
 AND owner_id=owner AND purpose='review_selection' FOR UPDATE;
 IF document.id IS NULL OR document.state<>'ready' OR document.artifact_id IS DISTINCT FROM p_artifact
 OR document.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex') OR document.bytes<>octet_length(p_body)
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready'
 AND a.sha256=document.sha256 AND a.bytes=document.bytes AND a.bucket=document.bucket AND a.object_version=document.object_version
 AND a.output_role='review_selection' AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'SEMANTIC_REJECTION_DOCUMENT_INVALID' USING ERRCODE='23514';END IF;
 IF request->>'run_id' IS DISTINCT FROM r.id::text OR (request-'run_id'-'request')<>'{}'::jsonb
 OR doc->>'run_id' IS DISTINCT FROM r.id::text OR doc->>'schema_version' IS DISTINCT FROM '1'
 OR (doc-'schema_version'-'kind'-'run_id'-'expected_revision'-'plan'-'intent'-'snapshot_id'-'targets')<>'{}'::jsonb
 OR ((request->'request')-'plan_sha'-'target_decisions'-'expected_revision')<>'{}'::jsonb
 OR jsonb_typeof(doc->'schema_version') IS DISTINCT FROM 'number'
 OR doc->>'kind' IS DISTINCT FROM 'semantic_review' OR doc->>'snapshot_id' IS DISTINCT FROM i.snapshot_id::text
 OR doc->>'expected_revision' IS DISTINCT FROM p_revision::text OR request->'request'->>'expected_revision' IS DISTINCT FROM p_revision::text
 OR request->'request'->>'plan_sha' IS DISTINCT FROM doc->'plan'->>'sha256'
 OR doc->'intent' IS DISTINCT FROM jsonb_build_object('artifact_id',i.intent_artifact_id,'sha256',i.intent_sha)
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='SemanticPlan'
 AND doc->'plan'=jsonb_build_object('artifact_id',d->>'artifact_id','sha256',d->>'sha256'))
 OR jsonb_typeof(doc->'targets') IS DISTINCT FROM 'array'
 OR jsonb_typeof(request->'request'->'target_decisions') IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'SEMANTIC_REJECTION_BINDING_INVALID' USING ERRCODE='23514';END IF;
 n:=jsonb_array_length(doc->'targets');
 IF n NOT BETWEEN 1 AND 100 OR jsonb_array_length(request->'request'->'target_decisions')<>n
 OR (SELECT count(DISTINCT d->>'target_id') FROM jsonb_array_elements(doc->'targets') d)<>n
 OR (SELECT count(DISTINCT d->>'target_id') FROM jsonb_array_elements(request->'request'->'target_decisions') d)<>n
 OR EXISTS(WITH candidates AS MATERIALIZED(SELECT d->>'artifact_id' AS id,d->>'sha256' AS sha,(d->>'body')::jsonb->'target' AS source
 FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='TransferCandidate')
 SELECT 1 FROM jsonb_array_elements(doc->'targets') d
 LEFT JOIN photo.semantic_targets t ON t.tenant_id=p_tenant AND t.intent_id=i.id AND t.id::text=d->>'target_id'
 LEFT JOIN candidates c ON c.id=t.candidate_artifact_id::text
 WHERE t.id IS NULL OR t.state<>'proposed' OR t.revision<>1 OR t.child_run_id IS NOT NULL OR t.batch_id IS NOT NULL
 OR (d-'target_id'-'target_revision'-'decision'-'source'-'candidate')<>'{}'::jsonb
 OR d->>'decision' IS DISTINCT FROM 'reject' OR d->>'target_revision' IS DISTINCT FROM t.revision::text
 OR d->'source' IS DISTINCT FROM c.source OR d->'candidate' IS DISTINCT FROM jsonb_build_object('artifact_id',c.id,'sha256',c.sha)
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(request->'request'->'target_decisions') q
 WHERE q->>'target_id'=t.id::text AND q->>'decision'='reject' AND (q-'target_id'-'decision')='{}'::jsonb)) THEN
  RAISE EXCEPTION 'SEMANTIC_REJECTION_TARGET_CONFLICT' USING ERRCODE='40001';END IF;
 INSERT INTO photo.v2_review_batches(id,tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action)
 VALUES(owner,p_tenant,i.project_id,i.workflow_id,p_artifact,p_actor,'decide');
 UPDATE photo.semantic_targets SET state='rejected',batch_id=owner WHERE tenant_id=p_tenant AND intent_id=i.id
 AND id::text IN(SELECT d->>'target_id' FROM jsonb_array_elements(doc->'targets') d);
 GET DIAGNOSTICS remaining=ROW_COUNT;
 IF remaining<>n THEN RAISE EXCEPTION 'SEMANTIC_REJECTION_PARTIAL' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET revision=revision+1 WHERE tenant_id=p_tenant AND id=r.id AND revision=p_revision AND status='waiting_review' AND NOT cancel_requested;
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
