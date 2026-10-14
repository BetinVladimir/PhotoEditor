-- +goose Up
-- Publish only previously qualified export evidence under its exact scheduler
-- lease. Links, inbox receipts and event are one transaction; run finalization
-- remains the scheduler finalizer's responsibility.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v30_export_adoption(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,p_body text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;w photo.v2_workflows%ROWTYPE;t photo.tasks%ROWTYPE;
 proof photo.v30_interchange_export_evidence%ROWTYPE;marker uuid;prior jsonb;
 inventory jsonb:='[]'::jsonb;ref jsonb;source jsonb;ordinal integer:=0;
 source_index integer:=0;purpose_path text;next_cursor bigint;next_revision bigint;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 16777216
  OR coalesce(p_digest,'') !~ '^[0-9a-f]{64}$'
  OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM p_digest
  OR p_tenant IS NULL THEN RETURN false;END IF;
 e:=p_body::jsonb;
 IF e->>'tenant_id' IS DISTINCT FROM p_tenant::text
  OR e->>'actor_id' IS DISTINCT FROM p_actor::text
  OR e->>'request_sha256' IS DISTINCT FROM p_request_sha
  OR e->'identity'->>'Run' IS DISTINCT FROM p_run::text
  OR e->'identity'->>'Workflow' IS DISTINCT FROM p_workflow::text
  OR e->'identity'->>'Task' IS DISTINCT FROM p_task::text THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR UPDATE;
 IF w.id IS NULL OR w.v30_export_adoption_token IS DISTINCT FROM p_token
  OR p_run IS DISTINCT FROM photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key)
  OR p_workflow IS DISTINCT FROM photo.v30_uuid5(p_run,'workflow')
  OR p_task IS DISTINCT FROM photo.v30_uuid5(p_workflow,'v2.interchange.export:0')
  THEN RETURN false;END IF;
 marker:=photo.v2_adoption_event_id(p_task,p_result);
 SELECT * INTO proof FROM photo.v30_interchange_export_evidence
  WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant
  AND consumer='v30-interchange-export' AND event_id=marker) THEN
  SELECT payload INTO prior FROM photo.run_events WHERE tenant_id=p_tenant AND run_id=p_run
   AND type='v2.task.adopted' AND payload->>'task_id'=p_task::text
   AND payload->>'result_id'=p_result::text;
  RETURN coalesce(proof.result_id=p_result AND proof.request_sha=p_request_sha
   AND proof.evidence_sha=p_digest AND proof.evidence_jcs=p_body
   AND prior->>'export_evidence_sha'=p_digest
   AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant
    AND consumer='v2-adopt' AND event_id=marker),false);
 END IF;
 IF photo.save_v30_export_adoption_evidence(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_body,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND id=p_task FOR UPDATE;
 SELECT * INTO proof FROM photo.v30_interchange_export_evidence
  WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF t.id IS NULL OR t.canonical_result_id IS DISTINCT FROM p_result
  OR proof.result_id IS DISTINCT FROM p_result OR proof.evidence_sha IS DISTINCT FROM p_digest
  OR proof.evidence_jcs IS DISTINCT FROM p_body
  OR EXISTS(SELECT 1 FROM photo.v2_artifact_links WHERE tenant_id=p_tenant
   AND workflow_id=p_workflow) THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(e->'documents') LOOP
  purpose_path:=CASE ordinal WHEN 0 THEN '$' WHEN 1 THEN '$/report'
   ELSE '$/report/shards/'||(ordinal-2)::text END;
  inventory:=inventory||jsonb_build_array(jsonb_build_object(
   'artifact_id',ref->>'artifact_id','sha256',ref->>'sha256','ordinal',ordinal,
   'purpose',encode(sha256(convert_to('tasks/'||p_task||'/'||purpose_path,'UTF8')),'hex')));
  ordinal:=ordinal+1;
 END LOOP;
 ref:=e->'bundle';
 inventory:=inventory||jsonb_build_array(jsonb_build_object(
  'artifact_id',ref->>'artifact_id','sha256',ref->>'sha256','ordinal',ordinal,
  'purpose',encode(sha256(convert_to('tasks/'||p_task||'/$/bundle','UTF8')),'hex')));
 ordinal:=ordinal+1;
 FOR source IN SELECT value FROM jsonb_array_elements(e->'sources') LOOP
  ref:=source->'ref';
  inventory:=inventory||jsonb_build_array(jsonb_build_object(
   'artifact_id',ref->>'artifact_id','sha256',ref->>'sha256','ordinal',ordinal,
   'purpose',encode(sha256(convert_to('tasks/'||p_task||'/$/portable_sources/'||source_index::text,'UTF8')),'hex')));
  ordinal:=ordinal+1;source_index:=source_index+1;
 END LOOP;
 IF (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(inventory))
  <>jsonb_array_length(inventory) THEN
  RAISE EXCEPTION 'V30_EXPORT_INVENTORY_CONFLICT' USING ERRCODE='23514';
 END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token)
 WHERE artifact_id=(e->'root'->>'artifact_id')::uuid AND sha256=e->'root'->>'sha256';
 IF NOT FOUND THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(inventory) LOOP
  INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal)
  VALUES(p_tenant,w.project_id,p_workflow,(ref->>'artifact_id')::uuid,
   ref->>'sha256',ref->>'purpose',(ref->>'ordinal')::integer);
 END LOOP;
 UPDATE photo.runs SET revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run
  AND revision<9007199254740991 RETURNING revision INTO next_revision;
 IF next_revision IS NULL THEN RAISE EXCEPTION 'RUN_REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
 VALUES(p_tenant,'v2-adopt',marker,clock_timestamp()),
  (p_tenant,'v30-interchange-export',marker,clock_timestamp());
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant
  AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,p_run,p_actor,'principal','v2.task.adopted',
  jsonb_build_object('task_id',p_task,'result_id',p_result,'attempt',t.attempt,
   'fence',t.fencing_token,'inventory_digest',p_digest,
   'sql_digest',encode(sha256(convert_to(inventory::text,'UTF8')),'hex'),
   'inventory_count',jsonb_array_length(inventory),'export_evidence_sha',p_digest));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,marker,'photo.events.v1','v2.task.adopted',
  jsonb_build_object('schema_version',1,'event_id',marker,'type','v2.task.adopted',
   'tenant_id',p_tenant,'run_id',p_run,'task_id',p_task,'result_id',p_result));
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v30_export_adoption(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_export_adoption(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_export_adoption(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
