-- +goose Up
-- Complete artifact adoption + exact draft refs + review opening atomically.
-- +goose StatementBegin
REVOKE EXECUTE ON FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) FROM photo_scheduler;
CREATE FUNCTION photo.adopt_v30_interchange_import(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_inventory jsonb,p_digest text,p_params text,p_root text,p_report text,p_shards jsonb,p_closures jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;r photo.runs%ROWTYPE;target jsonb;receipt jsonb;ref jsonb;project uuid;
 count_proposed integer:=0;total_refs bigint:=0;stream uuid;document uuid;cursor_value bigint;event_id uuid;payload jsonb;
BEGIN
 IF jsonb_typeof(p_closures) IS DISTINCT FROM 'array' OR jsonb_array_length(p_closures)>5000
 OR pg_column_size(p_closures)>134217728 THEN RETURN false;END IF;
 IF photo.adopt_v30_interchange_import(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest,p_params,p_root,p_report,p_shards) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND owner_id=p_task AND purpose='task_params' AND state='ready' FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=d.run_id FOR UPDATE;
 project:=(d.v30_import_review->>'project_id')::uuid;
 IF d.v30_import_review IS NULL OR project IS NULL OR r.status<>'running' OR r.cancel_requested OR r.revision>=9007199254740991 THEN
 RAISE EXCEPTION 'INTERCHANGE_REVIEW_OPEN_STALE' USING ERRCODE='23514';END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(p_closures) c GROUP BY c->'recipe' HAVING count(*)<>1) THEN
 RAISE EXCEPTION 'INTERCHANGE_REVIEW_DUPLICATE_CLOSURE' USING ERRCODE='23514';END IF;
 FOR target IN SELECT value FROM jsonb_array_elements(d.v30_import_review->'targets') WHERE value->>'state'='proposed' LOOP
  count_proposed:=count_proposed+1;
  SELECT value INTO receipt FROM jsonb_array_elements(p_closures) WHERE value->'recipe'=target->'recipe';
  IF receipt IS NULL OR jsonb_typeof(receipt->'refs') IS DISTINCT FROM 'array' OR jsonb_array_length(receipt->'refs')>30000 THEN
   RAISE EXCEPTION 'INTERCHANGE_REVIEW_CLOSURE_REQUIRED' USING ERRCODE='23514';END IF;
  total_refs:=total_refs+jsonb_array_length(receipt->'refs');
  IF total_refs>1000000 OR EXISTS(SELECT 1 FROM jsonb_array_elements(receipt->'refs') x GROUP BY x->>'artifact_id' HAVING count(*)<>1) THEN
   RAISE EXCEPTION 'INTERCHANGE_REVIEW_CLOSURE_LIMIT' USING ERRCODE='23514';END IF;
  IF EXISTS(SELECT 1 FROM jsonb_array_elements(receipt->'refs') WITH ORDINALITY f(value,ord)
   WHERE jsonb_typeof(value) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(value))<>4
   OR value->>'ordinal' IS DISTINCT FROM (ord-1)::text OR value->>'artifact_id'=target#>>'{recipe,artifact_id}'
   OR value->>'purpose' IS DISTINCT FROM encode(sha256(convert_to('interchange/recipe/'||(target#>>'{recipe,artifact_id}')||'/dependency/'||(value->>'artifact_id'),'UTF8')),'hex')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
    WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=(value->>'artifact_id')::uuid AND i.sha256=value->>'sha256'
    AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN
   RAISE EXCEPTION 'INTERCHANGE_REVIEW_CLOSURE_STALE' USING ERRCODE='23514';END IF;
  stream:=(target->>'stream_id')::uuid;document:=(target->>'document_id')::uuid;
  INSERT INTO photo.v2_document_streams(id,tenant_id,project_id,asset_version_id,stream_type)
   VALUES(stream,p_tenant,project,(target->>'asset_version_id')::uuid,'draft');
  INSERT INTO photo.v2_documents(id,tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256)
   VALUES(document,p_tenant,project,stream,1,'EditingRecipeV3',(target#>>'{recipe,artifact_id}')::uuid,target#>>'{recipe,sha256}');
  INSERT INTO photo.v2_document_refs(tenant_id,project_id,document_id,artifact_id,sha256,purpose,ordinal)
   SELECT p_tenant,project,document,(value->>'artifact_id')::uuid,value->>'sha256',value->>'purpose',(value->>'ordinal')::integer
   FROM jsonb_array_elements(receipt->'refs');
  UPDATE photo.v2_document_streams SET current_document_id=document WHERE tenant_id=p_tenant AND id=stream;
 END LOOP;
 IF count_proposed<>jsonb_array_length(p_closures) THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_EXTRA_CLOSURE' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status='waiting_review',revision=revision+1 WHERE tenant_id=p_tenant AND id=r.id AND status='running' AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_OPEN_STALE' USING ERRCODE='23514';END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF cursor_value IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=gen_random_uuid();payload:=jsonb_build_object('workflow_id',d.v30_import_review->'workflow_id','report',d.v30_import_review->'report','revision',(r.revision+1)::text,'proposed_count',count_proposed);
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(p_tenant,cursor_value,r.id,r.created_by,'principal','v30.interchange.review.opened',payload);
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(p_tenant,event_id,'photo.events.v1','v30.interchange.review.opened',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v30.interchange.review.opened','tenant_id',p_tenant,'run_id',r.id,'result',payload));
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
