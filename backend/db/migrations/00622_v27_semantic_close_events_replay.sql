-- +goose Up
-- Close decisions independently of admitted child execution. The caller owns
-- canonical request bytes and a ready immutable review_selection publication.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.commit_semantic_review_close(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_key text,p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.semantic_intents%ROWTYPE; w photo.v2_workflows%ROWTYPE; r photo.runs%ROWTYPE;
 receipt photo.idempotency_records%ROWTYPE; d photo.backend_documents%ROWTYPE;
 request jsonb; body jsonb; request_sha text; owner uuid; result jsonb; cursor_value bigint; event_id uuid;
BEGIN
 IF p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9223372036854775806
 OR p_request_raw IS NULL OR octet_length(p_request_raw) NOT BETWEEN 2 AND 65536
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 THEN
  RAISE EXCEPTION 'SEMANTIC_CLOSE_INVALID' USING ERRCODE='22023';END IF;
 request:=p_request_raw::jsonb;body:=p_body::jsonb;
 request_sha:=encode(sha256(convert_to(p_request_raw,'UTF8')),'hex');
 SELECT * INTO i FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF i.id IS NULL OR NOT photo.v2_editor_authorized(p_tenant,i.project_id,p_actor,p_session) THEN
  RAISE EXCEPTION 'SEMANTIC_CLOSE_FORBIDDEN' USING ERRCODE='42501';END IF;
 -- Same ordering as Apply: receipt, source parent, complete target inventory.
 SELECT * INTO receipt FROM photo.idempotency_records WHERE tenant_id=p_tenant
 AND principal_type='user' AND principal_id=p_actor AND route='SemanticIntent.CloseReview' AND key=p_key FOR UPDATE;
 IF receipt.id IS NULL OR receipt.request_hash IS DISTINCT FROM request_sha OR receipt.response_status IS NOT NULL THEN
  RAISE EXCEPTION 'SEMANTIC_CLOSE_RECEIPT_CONFLICT' USING ERRCODE='40001';END IF;
 IF photo.initialize_semantic_review(p_tenant,i.workflow_id,p_actor,p_session,p_revision,p_documents) IS DISTINCT FROM i.id THEN
  RAISE EXCEPTION 'SEMANTIC_CLOSE_SCOPE_CONFLICT' USING ERRCODE='40001';END IF;
 PERFORM * FROM photo.lock_semantic_decision_targets(p_tenant,i.workflow_id,p_actor,p_session,p_revision);
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=i.workflow_id;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id;
 IF EXISTS(SELECT 1 FROM photo.semantic_targets WHERE tenant_id=p_tenant AND intent_id=i.id AND state='proposed') THEN
  RAISE EXCEPTION 'SEMANTIC_REVIEW_INCOMPLETE' USING ERRCODE='40001';END IF;
 IF request IS DISTINCT FROM jsonb_build_object('run_id',r.id,'request',jsonb_build_object('expected_revision',p_revision::text,'plan_sha',body->>'plan_sha'))
 OR body IS DISTINCT FROM jsonb_build_object('schema_version',1,'kind','semantic_review_close','run_id',r.id,'expected_revision',p_revision::text,'plan_sha',body->>'plan_sha')
 OR coalesce(body->>'plan_sha','') !~ '^[0-9a-f]{64}$'
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_documents) x WHERE x->>'contract'='SemanticPlan' AND x->>'sha256'=body->>'plan_sha') THEN
  RAISE EXCEPTION 'SEMANTIC_CLOSE_BINDING_INVALID' USING ERRCODE='23514';END IF;
 owner:=photo.semantic_decision_document_owner(p_tenant,r.id,p_actor,request_sha);
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact FOR SHARE;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=r.id AND owner_id=owner AND purpose='review_selection' FOR UPDATE;
 IF d.id IS NULL OR d.state<>'ready' OR d.artifact_id IS DISTINCT FROM p_artifact
 OR d.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex') OR d.bytes<>octet_length(p_body)
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready'
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
 AND a.media_type='application/json' AND a.output_role='review_selection' AND a.verified_at IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'SEMANTIC_CLOSE_DOCUMENT_INVALID' USING ERRCODE='23514';END IF;
 INSERT INTO photo.v2_review_batches(id,tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action)
 VALUES(owner,p_tenant,i.project_id,i.workflow_id,p_artifact,p_actor,'close');
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=r.id
 AND status='waiting_review' AND revision=p_revision AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_CLOSE_STALE' USING ERRCODE='40001';END IF;
 result:=jsonb_build_object('run_id',r.id,'workflow_id',w.id,'revision',(p_revision+1)::text,'state','completed','outputs','[]'::jsonb,'error_code',NULL);
 UPDATE photo.idempotency_records SET response_status=200,response_body=result,resource_id=owner WHERE tenant_id=p_tenant AND id=receipt.id;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(p_tenant,'user',p_actor,'semantic.review.close','semantic_intent',i.id,receipt.id,result);
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF cursor_value IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=gen_random_uuid();
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,cursor_value,r.id,p_actor,'principal','v27.review.closed',result);
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v27.review.closed',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v27.review.closed','tenant_id',p_tenant,'run_id',r.id,'result',result));
 RETURN result;
END $$;
ALTER FUNCTION photo.commit_semantic_review_close(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_semantic_review_close(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.commit_semantic_review_close(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb) TO photo_api;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.semantic_close_replay(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text)
RETURNS TABLE(outcome text,response_body jsonb)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE identity record; receipt photo.idempotency_records%ROWTYPE; batch photo.v2_review_batches%ROWTYPE;
BEGIN
 IF p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$'
 OR coalesce(p_request_sha,'') !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'SEMANTIC_CLOSE_REPLAY_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO identity FROM photo.semantic_review_identity(p_tenant,p_intent,p_actor,p_session);
 SELECT * INTO receipt FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='SemanticIntent.CloseReview' AND key=p_key;
 IF receipt.id IS NULL THEN RETURN QUERY SELECT 'absent'::text,NULL::jsonb;RETURN;END IF;
 IF receipt.request_hash IS DISTINCT FROM p_request_sha THEN RETURN QUERY SELECT 'conflict'::text,NULL::jsonb;RETURN;END IF;
 IF receipt.response_status IS NULL THEN RETURN QUERY SELECT 'pending'::text,NULL::jsonb;RETURN;END IF;
 SELECT * INTO batch FROM photo.v2_review_batches WHERE tenant_id=p_tenant AND id=receipt.resource_id
 AND project_id=identity.project_id AND workflow_id=identity.workflow_id AND actor_id=p_actor AND action='close' AND child_run_id IS NULL;
 IF batch.id IS NULL OR receipt.response_status<>200
 OR receipt.response_body->>'run_id' IS DISTINCT FROM identity.run_id::text
 OR receipt.response_body->>'workflow_id' IS DISTINCT FROM identity.workflow_id::text
 OR receipt.response_body->>'state' IS DISTINCT FROM 'completed'
 OR receipt.response_body->'outputs' IS DISTINCT FROM '[]'::jsonb
 OR receipt.response_body->'error_code' IS DISTINCT FROM 'null'::jsonb THEN
  RETURN QUERY SELECT 'conflict'::text,NULL::jsonb;RETURN;END IF;
 RETURN QUERY SELECT 'replayed'::text,receipt.response_body;
END $$;
ALTER FUNCTION photo.semantic_close_replay(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_close_replay(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_close_replay(uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
