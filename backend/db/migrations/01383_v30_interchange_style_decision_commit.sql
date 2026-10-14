-- +goose Up
-- Registration and authored pointer approval share the same CAS/idempotency transaction.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.commit_v30_interchange_decisions(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid,p_key text,
 p_revision bigint,p_request text,p_artifact uuid,p_body text,p_resolved jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;request jsonb;body jsonb;request_sha text;owner uuid;run uuid;project uuid;decision jsonb;target jsonb;resolved jsonb;
 receipt photo.idempotency_records%ROWTYPE;d photo.backend_documents%ROWTYPE;source photo.backend_documents%ROWTYPE;
 stream photo.v2_document_streams%ROWTYPE;targets jsonb;ordinal integer;approvals integer:=0;remaining integer;result jsonb;cursor_value bigint;event_id uuid;
BEGIN
 IF p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 65536 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536
 OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$'
 OR octet_length(p_resolved::text)>134217728 OR jsonb_typeof(p_resolved) IS DISTINCT FROM 'array' OR jsonb_array_length(p_resolved)>100 THEN
 RAISE EXCEPTION 'INTERCHANGE_DECISION_INVALID' USING ERRCODE='22023';END IF;
 request:=p_request::jsonb;body:=p_body::jsonb;request_sha:=encode(sha256(convert_to(p_request,'UTF8')),'hex');
 SELECT * INTO receipt FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='Interchange.DecideImport' AND key=p_key FOR UPDATE;
 IF receipt.id IS NULL OR receipt.request_hash IS DISTINCT FROM request_sha OR receipt.response_status IS NOT NULL THEN
 RAISE EXCEPTION 'INTERCHANGE_DECISION_RECEIPT_CONFLICT' USING ERRCODE='40001';END IF;
 run:=photo.lock_v30_interchange_review_publication(p_tenant,p_workflow,p_actor,p_session,p_revision,body->>'report_sha');
 authority:=photo.v30_interchange_review_context(p_tenant,p_actor,p_session,p_workflow,p_revision,body->>'report_sha');
 project:=(authority->>'project_id')::uuid;
 IF authority IS NULL OR request IS DISTINCT FROM jsonb_build_object('workflow_id',p_workflow,'request',jsonb_build_object('expected_revision',p_revision::text,'report_sha',body->>'report_sha','decisions',body->'decisions'))
 OR body IS DISTINCT FROM jsonb_build_object('schema_version',1,'kind','interchange_review_decide','workflow_id',p_workflow,'expected_revision',p_revision::text,'report_sha',body->>'report_sha','decisions',body->'decisions')
 OR jsonb_typeof(body->'decisions') IS DISTINCT FROM 'array' OR jsonb_array_length(body->'decisions') NOT BETWEEN 1 AND 100
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(body->'decisions') x GROUP BY x->>'entry_id' HAVING count(*)<>1)
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(p_resolved) x GROUP BY x->>'entry_id' HAVING count(*)<>1) THEN
 RAISE EXCEPTION 'INTERCHANGE_DECISION_BINDING_INVALID' USING ERRCODE='23514';END IF;
 owner:=photo.v30_uuid5(p_workflow,'interchange:review:decide:'||p_actor::text||':'||request_sha);
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=run AND owner_id=owner AND purpose='review_selection' FOR UPDATE;
 IF d.id IS NULL OR d.state<>'ready' OR d.artifact_id IS DISTINCT FROM p_artifact OR d.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex') OR d.bytes<>octet_length(p_body)
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND a.media_type='application/json' AND a.output_role='review_selection' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
 RAISE EXCEPTION 'INTERCHANGE_DECISION_DOCUMENT_INVALID' USING ERRCODE='23514';END IF;
 SELECT * INTO source FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=run AND purpose='task_params'
 AND owner_id=photo.v30_uuid5(p_workflow,'v2.interchange.import:0') AND state='ready' FOR UPDATE;
 targets:=source.v30_import_review->'targets';
 FOR decision IN SELECT value FROM jsonb_array_elements(body->'decisions') LOOP
  IF jsonb_typeof(decision) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(decision))<>2
   OR NOT(decision ?& ARRAY['entry_id','decision']) OR decision->>'decision' IS NULL OR decision->>'decision' NOT IN('approve','reject') THEN
   RAISE EXCEPTION 'INTERCHANGE_DECISION_INVALID' USING ERRCODE='23514';END IF;
  SELECT value,(ord-1)::integer INTO target,ordinal FROM jsonb_array_elements(targets) WITH ORDINALITY x(value,ord) WHERE value->'entry_id'=decision->'entry_id';
  IF target IS NULL OR target->>'state' IS DISTINCT FROM 'proposed' THEN RAISE EXCEPTION 'INTERCHANGE_TARGET_STALE' USING ERRCODE='40001';END IF;
  SELECT * INTO stream FROM photo.v2_document_streams WHERE tenant_id=p_tenant AND project_id=project AND id=(target->>'stream_id')::uuid
   AND asset_version_id=(target->>'asset_version_id')::uuid AND stream_type='draft' FOR UPDATE;
  IF stream.id IS NULL OR stream.current_document_id IS DISTINCT FROM (target->>'document_id')::uuid OR stream.approved_document_id IS NOT NULL
   OR NOT EXISTS(SELECT 1 FROM photo.v2_documents WHERE tenant_id=p_tenant AND project_id=project AND stream_id=stream.id AND id=stream.current_document_id
   AND artifact_id=(target#>>'{recipe,artifact_id}')::uuid AND sha256=target#>>'{recipe,sha256}') THEN
   RAISE EXCEPTION 'INTERCHANGE_DRAFT_CHANGED' USING ERRCODE='40001';END IF;
  IF decision->>'decision'='approve' THEN
   SELECT value INTO resolved FROM jsonb_array_elements(p_resolved) WHERE value->'entry_id'=decision->'entry_id';
   IF resolved IS NULL OR (SELECT count(*) FROM jsonb_object_keys(resolved))<>CASE WHEN resolved ? 'style_registration' THEN 5 ELSE 4 END
    OR NOT(resolved ?& ARRAY['entry_id','stream_revision','artifact_id','sha256']) OR resolved->>'stream_revision' IS DISTINCT FROM stream.revision::text THEN
    RAISE EXCEPTION 'INTERCHANGE_RESOLVED_REQUIRED' USING ERRCODE='23514';END IF;
   IF resolved ? 'style_registration' THEN
    IF jsonb_typeof(resolved->'style_registration') IS DISTINCT FROM 'object'
    OR (SELECT count(*) FROM jsonb_object_keys(resolved->'style_registration'))<>4
    OR NOT(resolved->'style_registration' ?& ARRAY['parsed','binding','style','config']) THEN
    RAISE EXCEPTION 'INTERCHANGE_STYLE_REGISTRATION_INVALID' USING ERRCODE='23514';END IF;
    PERFORM photo.register_v30_interchange_style(p_tenant,p_actor,p_session,p_workflow,p_revision,body->>'report_sha',
     resolved#>>'{style_registration,parsed}',(decision->>'entry_id')::uuid,resolved#>'{style_registration,binding}',
     (resolved#>>'{style_registration,style,artifact_id}')::uuid,resolved#>>'{style_registration,style,sha256}',resolved#>>'{style_registration,config}');
   END IF;
   PERFORM photo.approve_v23_draft_with_sources(p_tenant,stream.asset_version_id,stream.revision,stream.current_document_id,p_actor,p_session,
    (resolved->>'artifact_id')::uuid,resolved->>'sha256',NULL::uuid,NULL::uuid,NULL::text,NULL::uuid,NULL::text,NULL::uuid,NULL::integer);
   approvals:=approvals+1;target:=jsonb_set(target,'{state}','"approved"'::jsonb);
  ELSE target:=jsonb_set(target,'{state}','"rejected"'::jsonb);END IF;
  targets:=jsonb_set(targets,ARRAY[ordinal::text],target);
 END LOOP;
 IF approvals<>jsonb_array_length(p_resolved) THEN RAISE EXCEPTION 'INTERCHANGE_RESOLVED_EXTRA' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET v30_import_review=jsonb_set(v30_import_review,'{targets}',targets) WHERE tenant_id=p_tenant AND id=source.id;
 INSERT INTO photo.v2_review_batches(id,tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action) VALUES(owner,p_tenant,project,p_workflow,p_artifact,p_actor,'decide');
 UPDATE photo.runs SET revision=revision+1 WHERE tenant_id=p_tenant AND id=run AND status='waiting_review' AND revision=p_revision AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_DECISION_STALE' USING ERRCODE='40001';END IF;
 SELECT count(*) INTO remaining FROM jsonb_array_elements(targets) t WHERE t->>'state'='proposed';
 result:=jsonb_build_object('workflow_id',p_workflow,'revision',(p_revision+1)::text,'remaining',remaining,'child',NULL,'batch_id',owner);
 UPDATE photo.idempotency_records SET response_status=200,response_body=result,resource_id=owner WHERE tenant_id=p_tenant AND id=receipt.id;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES(p_tenant,'user',p_actor,'interchange.review.decide','interchange_job',p_workflow,receipt.id,result);
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF cursor_value IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=gen_random_uuid();
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(p_tenant,cursor_value,run,p_actor,'principal','v30.interchange.review.decided',result);
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(p_tenant,event_id,'photo.events.v1','v30.interchange.review.decided',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v30.interchange.review.decided','tenant_id',p_tenant,'run_id',run,'result',result));
 RETURN result;
END $$;
ALTER FUNCTION photo.commit_v30_interchange_decisions(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_v30_interchange_decisions(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.commit_v30_interchange_decisions(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
