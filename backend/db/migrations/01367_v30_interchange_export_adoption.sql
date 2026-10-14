-- +goose Up
-- Dedicated export adoption: verified immutable evidence and current receipts
-- are rechecked before links, inbox and events become visible atomically.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v30_interchange_export(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_body text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;rid uuid;wid uuid;tid uuid;t photo.tasks%ROWTYPE;proof photo.v30_interchange_export_evidence%ROWTYPE;
 marker uuid;inventory jsonb:='[]'::jsonb;ref jsonb;source jsonb;ordinal integer:=0;index integer:=0;purpose_path text;
 project uuid;next_cursor bigint;next_revision bigint;prior jsonb;
BEGIN
 -- Save also locks tenant/ledger/run/task and verifies exact replay, current
 -- canonical task outputs/source refs/ACL/object pins. No IO occurs in SQL.
 IF photo.save_v30_interchange_export_evidence(p_tenant,p_actor,p_session,p_key,p_request_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 e:=p_body::jsonb;rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');tid:=photo.v30_uuid5(wid,'v2.interchange.export:0');
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid AND id=tid FOR UPDATE;
 SELECT project_id INTO project FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=rid AND id=wid;
 SELECT * INTO proof FROM photo.v30_interchange_export_evidence WHERE tenant_id=p_tenant AND run_id=rid FOR SHARE;
 IF t.id IS NULL OR project IS NULL OR proof.result_id IS DISTINCT FROM t.canonical_result_id OR proof.evidence_sha IS DISTINCT FROM p_digest THEN RETURN false;END IF;
 marker:=photo.v2_adoption_event_id(t.id,t.canonical_result_id);
 IF EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt' AND event_id=marker) THEN
  SELECT payload INTO prior FROM photo.run_events WHERE tenant_id=p_tenant AND run_id=rid AND type='v2.task.adopted'
  AND payload->>'task_id'=tid::text AND payload->>'result_id'=t.canonical_result_id::text;
  RETURN coalesce(prior->>'export_evidence_sha'=p_digest,false) AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-interchange-export' AND event_id=marker);
 END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_artifact_links WHERE tenant_id=p_tenant AND workflow_id=wid) THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(e->'documents') LOOP
  purpose_path:=CASE ordinal WHEN 0 THEN '$' WHEN 1 THEN '$/report' ELSE '$/report/shards/'||(ordinal-2)::text END;
  inventory:=inventory||jsonb_build_array(jsonb_build_object('artifact_id',ref->>'artifact_id','sha256',ref->>'sha256','ordinal',ordinal,
   'purpose',encode(sha256(convert_to('tasks/'||tid||'/'||purpose_path,'UTF8')),'hex')));ordinal:=ordinal+1;
 END LOOP;
 ref:=e->'bundle';inventory:=inventory||jsonb_build_array(jsonb_build_object('artifact_id',ref->>'artifact_id','sha256',ref->>'sha256','ordinal',ordinal,
  'purpose',encode(sha256(convert_to('tasks/'||tid||'/$/bundle','UTF8')),'hex')));ordinal:=ordinal+1;
 FOR source IN SELECT value FROM jsonb_array_elements(e->'sources') LOOP
  ref:=source->'ref';inventory:=inventory||jsonb_build_array(jsonb_build_object('artifact_id',ref->>'artifact_id','sha256',ref->>'sha256','ordinal',ordinal,
   'purpose',encode(sha256(convert_to('tasks/'||tid||'/$/portable_sources/'||index::text,'UTF8')),'hex')));ordinal:=ordinal+1;index:=index+1;
 END LOOP;
 IF (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(inventory))<>jsonb_array_length(inventory) THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(inventory) LOOP
  INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal)
  VALUES(p_tenant,project,wid,(ref->>'artifact_id')::uuid,ref->>'sha256',ref->>'purpose',(ref->>'ordinal')::integer);
 END LOOP;
 UPDATE photo.runs SET revision=revision+1 WHERE tenant_id=p_tenant AND id=rid AND revision<9007199254740991 RETURNING revision INTO next_revision;
 IF next_revision IS NULL THEN RAISE EXCEPTION 'RUN_REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(p_tenant,'v2-adopt',marker,clock_timestamp()),(p_tenant,'v30-interchange-export',marker,clock_timestamp());
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,rid,p_actor,'principal','v2.task.adopted',jsonb_build_object('task_id',tid,'result_id',t.canonical_result_id,'attempt',t.attempt,'fence',t.fencing_token,
  'inventory_digest',p_digest,'sql_digest',encode(sha256(convert_to(inventory::text,'UTF8')),'hex'),'inventory_count',jsonb_array_length(inventory),'export_evidence_sha',p_digest));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(p_tenant,marker,'photo.events.v1','v2.task.adopted',
 jsonb_build_object('schema_version',1,'event_id',marker,'type','v2.task.adopted','tenant_id',p_tenant,'run_id',rid,'task_id',tid,'result_id',t.canonical_result_id));
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v30_interchange_export(uuid,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_interchange_export(uuid,uuid,uuid,text,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_interchange_export(uuid,uuid,uuid,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
