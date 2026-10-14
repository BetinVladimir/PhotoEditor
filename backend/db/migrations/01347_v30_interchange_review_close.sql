-- +goose Up
-- Caller owns canonical request, ready review_selection and a pending idempotency record.
-- +goose StatementBegin
CREATE FUNCTION photo.commit_v30_interchange_review_close(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid,p_key text,
 p_revision bigint,p_request text,p_artifact uuid,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;request jsonb;body jsonb;request_sha text;owner uuid;run uuid;project uuid;
 receipt photo.idempotency_records%ROWTYPE;d photo.backend_documents%ROWTYPE;result jsonb;cursor_value bigint;event_id uuid;
BEGIN
 IF p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 65536 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536
 OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$' THEN RAISE EXCEPTION 'INTERCHANGE_CLOSE_INVALID' USING ERRCODE='22023';END IF;
 request:=p_request::jsonb;body:=p_body::jsonb;request_sha:=encode(sha256(convert_to(p_request,'UTF8')),'hex');
 SELECT * INTO receipt FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='Interchange.CloseReview' AND key=p_key FOR UPDATE;
 IF receipt.id IS NULL OR receipt.request_hash IS DISTINCT FROM request_sha OR receipt.response_status IS NOT NULL THEN
 RAISE EXCEPTION 'INTERCHANGE_CLOSE_RECEIPT_CONFLICT' USING ERRCODE='40001';END IF;
 run:=photo.lock_v30_interchange_review_publication(p_tenant,p_workflow,p_actor,p_session,p_revision,body->>'report_sha');
 authority:=photo.v30_interchange_review_context(p_tenant,p_actor,p_session,p_workflow,p_revision,body->>'report_sha');
 project:=(authority->>'project_id')::uuid;
 IF authority IS NULL OR EXISTS(SELECT 1 FROM jsonb_array_elements(authority#>'{review,targets}') t WHERE t->>'state'='proposed') THEN
 RAISE EXCEPTION 'INTERCHANGE_REVIEW_INCOMPLETE' USING ERRCODE='40001';END IF;
 IF request IS DISTINCT FROM jsonb_build_object('workflow_id',p_workflow,'request',jsonb_build_object('expected_revision',p_revision::text,'report_sha',body->>'report_sha'))
 OR body IS DISTINCT FROM jsonb_build_object('schema_version',1,'kind','interchange_review_close','workflow_id',p_workflow,'expected_revision',p_revision::text,'report_sha',body->>'report_sha') THEN
 RAISE EXCEPTION 'INTERCHANGE_CLOSE_BINDING_INVALID' USING ERRCODE='23514';END IF;
 owner:=photo.v30_uuid5(p_workflow,'interchange:review:close:'||p_actor::text||':'||request_sha);
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=run AND owner_id=owner AND purpose='review_selection' FOR UPDATE;
 IF d.id IS NULL OR d.state<>'ready' OR d.artifact_id IS DISTINCT FROM p_artifact
 OR d.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex') OR d.bytes<>octet_length(p_body)
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND a.media_type='application/json' AND a.output_role='review_selection' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
 RAISE EXCEPTION 'INTERCHANGE_CLOSE_DOCUMENT_INVALID' USING ERRCODE='23514';END IF;
 INSERT INTO photo.v2_review_batches(id,tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action)
 VALUES(owner,p_tenant,project,p_workflow,p_artifact,p_actor,'close');
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=run AND revision=p_revision AND status='waiting_review' AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_CLOSE_STALE' USING ERRCODE='40001';END IF;
 result:=jsonb_build_object('run_id',run,'workflow_id',p_workflow,'revision',(p_revision+1)::text,'state','completed','outputs','[]'::jsonb,'error_code',NULL);
 UPDATE photo.idempotency_records SET response_status=200,response_body=result,resource_id=p_workflow WHERE tenant_id=p_tenant AND id=receipt.id;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(p_tenant,'user',p_actor,'interchange.review.close','interchange_job',p_workflow,receipt.id,result);
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF cursor_value IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=gen_random_uuid();
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(p_tenant,cursor_value,run,p_actor,'principal','v30.interchange.review.closed',result);
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(p_tenant,event_id,'photo.events.v1','v30.interchange.review.closed',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v30.interchange.review.closed','tenant_id',p_tenant,'run_id',run,'result',result));
 RETURN result;
END $$;
ALTER FUNCTION photo.commit_v30_interchange_review_close(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_v30_interchange_review_close(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.commit_v30_interchange_review_close(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
