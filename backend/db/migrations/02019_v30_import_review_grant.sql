-- +goose Up
-- A current editor grants a decision for an already published target proposal.
-- The archived review is never an input to this function.
-- +goose StatementBegin
CREATE FUNCTION photo.grant_v30_import_review(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_request_id uuid,
 p_parsed text,p_proposal_raw text,p_request jsonb)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;sources jsonb;
 entry_body jsonb;proposal jsonb;scope jsonb;candidate jsonb;item jsonb;
 old photo.v30_import_review_decisions%ROWTYPE;
 entry_id uuid;target_version uuid;proposal_id uuid;base_id uuid;label_id uuid;
 proposal_sha text;base_sha text;label_sha text;kind text;grant_decision text;
 accepted jsonb;rejected jsonb;seen text[]:=ARRAY[]::text[];candidate_id text;
 expected_epoch bigint;next_epoch bigint;decision_hash text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_request_id IS NULL
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR p_proposal_raw IS NULL OR octet_length(p_proposal_raw) NOT BETWEEN 2 AND 1048576
 OR p_request IS NULL OR jsonb_typeof(p_request) IS DISTINCT FROM 'object'
 OR p_request->>'action' IS DISTINCT FROM 'grant' THEN RETURN NULL;END IF;
 kind:=p_request->>'kind';grant_decision:=p_request->>'decision';
 IF kind IS NULL OR grant_decision IS NULL OR kind NOT IN('defects','dust') OR grant_decision NOT IN('accept','reject','skip')
 OR coalesce(p_request->>'expected_epoch','') !~ '^(0|[1-9][0-9]*)$' THEN RETURN NULL;END IF;
 expected_epoch:=(p_request->>'expected_epoch')::bigint;
 IF expected_epoch>=9007199254740991 THEN RETURN NULL;END IF;
 entry_id:=(p_request->>'entry_id')::uuid;
 target_version:=(p_request->>'target_version_id')::uuid;
 proposal_id:=(p_request#>>'{proposal,artifact_id}')::uuid;
 proposal_sha:=p_request#>>'{proposal,sha256}';
 base_id:=(p_request#>>'{base,artifact_id}')::uuid;
 base_sha:=p_request#>>'{base,sha256}';
 IF entry_id IS NULL OR target_version IS NULL OR proposal_id IS NULL OR base_id IS NULL
 OR proposal_id=base_id OR coalesce(proposal_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(base_sha,'') !~ '^[0-9a-f]{64}$'
 OR proposal_sha IS DISTINCT FROM encode(sha256(convert_to(p_proposal_raw,'UTF8')),'hex') THEN RETURN NULL;END IF;
 IF kind='defects' THEN
  label_id:=(p_request#>>'{label_map,artifact_id}')::uuid;
  label_sha:=p_request#>>'{label_map,sha256}';
  IF label_id IS NULL OR label_id IN(proposal_id,base_id)
  OR coalesce(label_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 ELSIF p_request ? 'label_map' THEN RETURN NULL;END IF;
 accepted:=coalesce(p_request->'accepted_candidate_ids','[]'::jsonb);
 rejected:=coalesce(p_request->'rejected_candidate_ids','[]'::jsonb);
 IF jsonb_typeof(accepted) IS DISTINCT FROM 'array' OR jsonb_typeof(rejected) IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 IF jsonb_array_length(accepted)+jsonb_array_length(rejected)>4096
 OR (grant_decision<>'accept' AND (jsonb_array_length(accepted)>0 OR jsonb_array_length(rejected)>0)) THEN RETURN NULL;END IF;
 proposal:=p_proposal_raw::jsonb;
 IF jsonb_typeof(proposal) IS DISTINCT FROM 'object' OR jsonb_typeof(proposal->'candidates') IS DISTINCT FROM 'array'
 OR jsonb_array_length(proposal->'candidates')>4096 THEN RETURN NULL;END IF;
 IF kind='defects' THEN
  IF proposal->>'version' IS DISTINCT FROM 'defect_candidates_v1'
  OR proposal#>>'{image,artifact_id}' IS DISTINCT FROM base_id::text
  OR proposal#>>'{image,sha256}' IS DISTINCT FROM base_sha
  OR proposal#>>'{label_map,artifact_id}' IS DISTINCT FROM label_id::text
  OR proposal#>>'{label_map,sha256}' IS DISTINCT FROM label_sha THEN RETURN NULL;END IF;
 ELSE
  IF proposal->>'algorithm' IS DISTINCT FROM 'sensor_dust_log_v1'
  OR jsonb_typeof(proposal->'frames') IS DISTINCT FROM 'array'
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(proposal->'frames') f
   WHERE f->>'artifact_id'=base_id::text AND f->>'sha256'=base_sha) THEN RETURN NULL;END IF;
 END IF;
 FOR candidate IN SELECT value FROM jsonb_array_elements(proposal->'candidates') LOOP
  IF coalesce(candidate->>'id','') !~ '^[0-9a-f]{64}$'
  OR candidate->>'decision' IS DISTINCT FROM 'pending' THEN RETURN NULL;END IF;
 END LOOP;
 FOR item IN SELECT value FROM jsonb_array_elements(accepted || rejected) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'string' THEN RETURN NULL;END IF;
  candidate_id:=trim(both '"' from item::text);
  IF candidate_id !~ '^[0-9a-f]{64}$' OR candidate_id=ANY(seen)
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(proposal->'candidates') c WHERE c->>'id'=candidate_id)
  THEN RETURN NULL;END IF;
  seen:=array_append(seen,candidate_id);
 END LOOP;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND workflow_type='interchange' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR SHARE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN NULL;END IF;
 SELECT value INTO entry_body FROM jsonb_array_elements(sources->'entries')
 WHERE value->>'entry_id'=entry_id::text AND value->>'state'='adopted'
 AND value->>'asset_version_id'=target_version::text;
 IF entry_body IS NULL OR entry_body#>>'{master,artifact_id}' IS DISTINCT FROM base_id::text
 OR entry_body#>>'{master,sha256}' IS DISTINCT FROM base_sha THEN RETURN NULL;END IF;
 SELECT b.v30_import_prepared INTO scope FROM photo.backend_documents b
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=p_tenant AND b.run_id=w.run_id AND b.artifact_id=proposal_id
 AND b.purpose='v30_import_prepared' AND b.state='ready' AND b.sha256=proposal_sha
 AND a.state='ready' AND a.sha256=proposal_sha AND a.output_role='v30_import_prepared'
 AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF scope IS NULL OR scope->>'contract' IS DISTINCT FROM CASE WHEN kind='defects' THEN 'DefectCandidates' ELSE 'DustCandidates' END
 OR scope->>'workflow' IS DISTINCT FROM p_workflow::text OR scope->>'entry' IS DISTINCT FROM entry_id::text
 OR scope#>>'{binding,asset_version_id}' IS DISTINCT FROM target_version::text
 OR scope#>>'{binding,source,artifact_id}' IS DISTINCT FROM base_id::text
 OR scope#>>'{binding,source,sha256}' IS DISTINCT FROM base_sha THEN RETURN NULL;END IF;
 decision_hash:=encode(sha256(convert_to(p_tenant::text||':'||p_workflow::text||':'||p_actor::text||':'||p_request::text,'UTF8')),'hex');
 SELECT * INTO old FROM photo.v30_import_review_decisions d WHERE d.tenant_id=p_tenant
 AND d.workflow_id=p_workflow AND d.entry_id=entry_id AND d.target_version_id=target_version
 AND d.kind=kind AND d.proposal_id=proposal_id FOR UPDATE;
 IF FOUND THEN
  IF old.project_id<>w.project_id OR old.proposal_sha<>proposal_sha OR old.base_id<>base_id OR old.base_sha<>base_sha
  OR old.label_map_id IS DISTINCT FROM label_id OR old.label_map_sha IS DISTINCT FROM label_sha THEN RETURN NULL;END IF;
  IF old.status='granted' AND old.request_id=p_request_id AND old.request_hash=decision_hash THEN RETURN old.epoch;END IF;
  IF old.epoch<>expected_epoch THEN RETURN NULL;END IF;
  UPDATE photo.v30_import_review_decisions AS d SET actor_id=p_actor,request_id=p_request_id,
   decision=grant_decision,accepted_ids=accepted,rejected_ids=rejected,decision_hash=decision_hash,request_hash=decision_hash,
   status='granted',epoch=epoch+1,updated_at=clock_timestamp()
  WHERE d.tenant_id=p_tenant AND d.workflow_id=p_workflow AND d.entry_id=entry_id
   AND d.target_version_id=target_version AND d.kind=kind AND d.proposal_id=proposal_id
  RETURNING epoch INTO next_epoch;
  UPDATE photo.v2_workflows SET v30_import_retry_after=NULL WHERE tenant_id=p_tenant AND id=p_workflow;
  RETURN next_epoch;
 END IF;
 IF expected_epoch<>0 THEN RETURN NULL;END IF;
 INSERT INTO photo.v30_import_review_decisions(tenant_id,workflow_id,project_id,entry_id,
  target_version_id,kind,proposal_id,proposal_sha,base_id,base_sha,label_map_id,label_map_sha,
  actor_id,request_id,decision,accepted_ids,rejected_ids,decision_hash,request_hash,status,epoch)
 VALUES(p_tenant,p_workflow,w.project_id,entry_id,target_version,kind,proposal_id,proposal_sha,
  base_id,base_sha,label_id,label_sha,p_actor,p_request_id,grant_decision,accepted,rejected,decision_hash,decision_hash,'granted',1);
 UPDATE photo.v2_workflows SET v30_import_retry_after=NULL WHERE tenant_id=p_tenant AND id=p_workflow;
 RETURN 1;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value
 OR unique_violation THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.grant_v30_import_review(uuid,uuid,uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.grant_v30_import_review(uuid,uuid,uuid,uuid,uuid,text,text,jsonb)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.grant_v30_import_review(uuid,uuid,uuid,uuid,uuid,text,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
