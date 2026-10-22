-- +goose Up
-- Private preview-aware approved/rejected decision gate; original gate retained. The final commit must hold
-- its receipt first, then call this before child creation or target changes.
-- This gate validates source initialization, locks targets, and checks exact
-- ready publication bytes/metadata and requested decisions. No batch/child,
-- quota, event, receipt or target transition is created here.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_shown_decision_batch(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE i photo.semantic_intents%ROWTYPE; w photo.v2_workflows%ROWTYPE; r photo.runs%ROWTYPE;
 document photo.backend_documents%ROWTYPE;request_sha text;request jsonb;doc jsonb;owner uuid;n integer;selected jsonb;ref jsonb;key text;
BEGIN
 IF p_request_raw IS NULL OR octet_length(p_request_raw) NOT BETWEEN 2 AND 65536
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9223372036854775806 THEN
  RAISE EXCEPTION 'SEMANTIC_DECISION_INVALID' USING ERRCODE='22023';END IF;
 request_sha:=encode(sha256(convert_to(p_request_raw,'UTF8')),'hex');
 request:=p_request_raw::jsonb;doc:=p_body::jsonb;
 SELECT * INTO i FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF i.id IS NULL OR NOT photo.v2_editor_authorized(p_tenant,i.project_id,p_actor,p_session) THEN
  RAISE EXCEPTION 'SEMANTIC_DECISION_FORBIDDEN' USING ERRCODE='42501';END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=i.workflow_id;
 IF photo.initialize_semantic_review(p_tenant,i.workflow_id,p_actor,p_session,p_revision,p_documents) IS DISTINCT FROM i.id THEN
  RAISE EXCEPTION 'SEMANTIC_DECISION_INTENT_CONFLICT' USING ERRCODE='23514';END IF;
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
  RAISE EXCEPTION 'SEMANTIC_DECISION_DOCUMENT_INVALID' USING ERRCODE='23514';END IF;
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
  RAISE EXCEPTION 'SEMANTIC_DECISION_BINDING_INVALID' USING ERRCODE='23514';END IF;
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
 OR (d-'target_id'-'target_revision'-'decision'-'source'-'candidate'-'preview')<>'{}'::jsonb
 OR d->>'decision' NOT IN('approve','reject') OR jsonb_typeof(d->'decision') IS DISTINCT FROM 'string' OR d->>'target_revision' IS DISTINCT FROM t.revision::text
 OR d->'source' IS DISTINCT FROM c.source OR d->'candidate' IS DISTINCT FROM jsonb_build_object('artifact_id',c.id,'sha256',c.sha)
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(request->'request'->'target_decisions') q
 WHERE q->>'target_id'=t.id::text AND q->>'decision'=d->>'decision' AND (q-'target_id'-'decision'-'preview')='{}'::jsonb
 AND q->'preview' IS NOT DISTINCT FROM d->'preview')) THEN
  RAISE EXCEPTION 'SEMANTIC_DECISION_TARGET_CONFLICT' USING ERRCODE='40001';END IF;
 -- Every approval names the exact shown result. Rejections carry no preview.
 FOR selected IN SELECT value FROM jsonb_array_elements(doc->'targets') ORDER BY value->>'target_id' LOOP
  IF selected->>'decision'='reject' THEN
   IF selected ? 'preview' THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_DECISION_INVALID' USING ERRCODE='22023';END IF;
   CONTINUE;
  END IF;
  IF jsonb_typeof(selected->'preview') IS DISTINCT FROM 'object'
  OR NOT(selected->'preview' ?& ARRAY['render','evidence'])
  OR ((selected->'preview')-'render'-'evidence')<>'{}'::jsonb THEN
   RAISE EXCEPTION 'SEMANTIC_SHOWN_DECISION_INVALID' USING ERRCODE='22023';END IF;
  FOREACH key IN ARRAY ARRAY['render','evidence'] LOOP
   ref:=selected->'preview'->key;
   IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT(ref ?& ARRAY['artifact_id','sha256'])
   OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb
   OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
   OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN
    RAISE EXCEPTION 'SEMANTIC_SHOWN_DECISION_INVALID' USING ERRCODE='22023';END IF;
  END LOOP;
  PERFORM * FROM photo.lock_semantic_preapply_preview_selection(p_tenant,p_intent,p_actor,p_session,p_revision,
   (selected->>'target_id')::uuid,(selected->>'target_revision')::bigint,doc->'plan'->>'sha256',selected->'candidate'->>'sha256',
   (selected->'preview'->'render'->>'artifact_id')::uuid,selected->'preview'->'render'->>'sha256',
   (selected->'preview'->'evidence'->>'artifact_id')::uuid,selected->'preview'->'evidence'->>'sha256');
  IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_DECISION_STALE' USING ERRCODE='40001';END IF;
 END LOOP;
 RETURN owner;
END $$;
ALTER FUNCTION photo.lock_semantic_shown_decision_batch(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_shown_decision_batch(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
