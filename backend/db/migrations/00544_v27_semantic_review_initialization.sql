-- +goose Up
-- Derive persistence from authenticated document bytes, never caller projections.
-- +goose StatementBegin
CREATE FUNCTION photo.initialize_semantic_review(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid,p_revision bigint,p_documents jsonb)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE; r photo.runs%ROWTYPE; existing photo.semantic_intents%ROWTYPE;
 inventory jsonb; doc jsonb; label text; plan jsonb; params jsonb; intent jsonb; snapshot jsonb; calibration jsonb;
 intent_id uuid; intent_sha text; reference_id uuid; count_targets integer; count_existing integer;
BEGIN
 IF jsonb_typeof(p_documents) IS DISTINCT FROM 'array' THEN RAISE EXCEPTION 'SEMANTIC_DOCUMENTS_INVALID' USING ERRCODE='22023';END IF;
 IF jsonb_array_length(p_documents) NOT BETWEEN 6 AND 5005 OR octet_length(p_documents::text)>134217728 THEN
  RAISE EXCEPTION 'SEMANTIC_DOCUMENTS_LIMIT' USING ERRCODE='22023';END IF;
 FOR doc IN SELECT value FROM jsonb_array_elements(p_documents) LOOP
  IF jsonb_typeof(doc) IS DISTINCT FROM 'object' OR (doc-'contract'-'artifact_id'-'sha256'-'body')<>'{}'::jsonb
   OR jsonb_typeof(doc->'contract') IS DISTINCT FROM 'string' OR jsonb_typeof(doc->'artifact_id') IS DISTINCT FROM 'string'
   OR jsonb_typeof(doc->'sha256') IS DISTINCT FROM 'string' OR jsonb_typeof(doc->'body') IS DISTINCT FROM 'string'
   OR coalesce(doc->>'sha256','') !~ '^[0-9a-f]{64}$' OR octet_length(doc->>'body') NOT BETWEEN 2 AND 8388608
   OR doc->>'contract' NOT IN('SemanticPlan','Params','InputSnapshot','EditIntent','Calibration','TransferCandidate') THEN
   RAISE EXCEPTION 'SEMANTIC_DOCUMENT_INVALID' USING ERRCODE='22023';END IF;
 END LOOP;
 IF (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(p_documents))<>jsonb_array_length(p_documents) THEN
  RAISE EXCEPTION 'SEMANTIC_DOCUMENT_DUPLICATE' USING ERRCODE='22023';END IF;
 FOREACH label IN ARRAY ARRAY['SemanticPlan','Params','InputSnapshot','EditIntent','Calibration'] LOOP
  IF (SELECT count(*) FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'=label)<>1 THEN
   RAISE EXCEPTION 'SEMANTIC_DOCUMENT_COVERAGE_INVALID' USING ERRCODE='22023';END IF;
 END LOOP;
 SELECT (d->>'body')::jsonb,d->>'sha256' INTO intent,intent_sha FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='EditIntent';
 reference_id:=(intent->'reference'->>'asset_version_id')::uuid;
 IF NOT photo.lock_semantic_review_sources(p_tenant,p_workflow,p_actor,p_session,p_revision,reference_id) THEN
  RAISE EXCEPTION 'SEMANTIC_REVIEW_STALE' USING ERRCODE='40001';END IF;
 SELECT jsonb_agg(to_jsonb(s)) INTO inventory FROM photo.semantic_review_plan_sources(p_tenant,p_workflow,p_actor,p_session) s;
 IF inventory IS NULL OR jsonb_array_length(inventory)<>jsonb_array_length(p_documents) THEN
  RAISE EXCEPTION 'SEMANTIC_DOCUMENT_SCOPE_INVALID' USING ERRCODE='23514';END IF;
 IF EXISTS(WITH supplied AS MATERIALIZED(SELECT * FROM jsonb_to_recordset(p_documents) AS d(contract text,artifact_id text,sha256 text,body text)),
  expected AS MATERIALIZED(SELECT * FROM jsonb_to_recordset(inventory) AS s(contract text,artifact_id text,sha256 text,bytes bigint))
  SELECT 1 FROM supplied d LEFT JOIN expected s ON s.contract=d.contract AND s.artifact_id=d.artifact_id AND s.sha256=d.sha256
  WHERE s.artifact_id IS NULL OR octet_length(d.body)<>s.bytes OR encode(public.digest(convert_to(d.body,'UTF8'),'sha256'),'hex')<>s.sha256) THEN
  RAISE EXCEPTION 'SEMANTIC_DOCUMENT_BYTES_INVALID' USING ERRCODE='23514';END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id;
 SELECT (d->>'body')::jsonb INTO plan FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='SemanticPlan';
 SELECT (d->>'body')::jsonb INTO params FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='Params';
 SELECT (d->>'body')::jsonb INTO snapshot FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='InputSnapshot';
 SELECT (d->>'body')::jsonb INTO calibration FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='Calibration';
 IF coalesce(plan->>'schema_version','')<>'1' OR coalesce(params->>'schema_version','')<>'1'
  OR coalesce(intent->>'schema_version','')<>'1' OR coalesce(snapshot->>'schema_version','')<>'1'
  OR coalesce(calibration->>'schema_version','')<>'1' OR jsonb_typeof(plan->'candidates') IS DISTINCT FROM 'array'
  OR jsonb_typeof(params->'candidates') IS DISTINCT FROM 'array' OR jsonb_typeof(snapshot->'items') IS DISTINCT FROM 'array'
  OR plan->>'intent_sha' IS DISTINCT FROM intent_sha
  OR plan->>'model_lock_sha' IS DISTINCT FROM r.config_snapshot->'v27_semantic'->'qualification'->>'model_lock_sha'
  OR params->>'model_lock_sha' IS DISTINCT FROM plan->>'model_lock_sha' OR calibration->>'model_lock_sha' IS DISTINCT FROM plan->>'model_lock_sha'
  OR calibration->>'dataset_sha' IS DISTINCT FROM r.config_snapshot->'v27_semantic'->'qualification'->>'dataset_sha'
  OR calibration->'qualified' IS DISTINCT FROM 'true'::jsonb
  OR snapshot->>'snapshot_id' IS DISTINCT FROM w.input_snapshot_id::text OR intent->>'target_snapshot_id' IS DISTINCT FROM w.input_snapshot_id::text
  OR snapshot->>'snapshot_digest' IS DISTINCT FROM r.config_snapshot->'v27_semantic'->>'snapshot_digest'
  OR plan->>'snapshot_digest' IS DISTINCT FROM snapshot->>'snapshot_digest'
  OR params->'calibration' IS DISTINCT FROM plan->'calibration' THEN
  RAISE EXCEPTION 'SEMANTIC_DOCUMENT_BINDING_INVALID' USING ERRCODE='23514';END IF;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='EditIntent'
  AND params->'intent'=jsonb_build_object('artifact_id',d->>'artifact_id','sha256',d->>'sha256'))
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='Calibration'
   AND params->'calibration'=jsonb_build_object('artifact_id',d->>'artifact_id','sha256',d->>'sha256'))
  OR NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
   WHERE v.tenant_id=p_tenant AND v.id=reference_id AND asset.project_id=w.project_id
    AND intent->'reference'->'source'=jsonb_build_object('artifact_id',v.master_artifact_id::text,'sha256',v.content_hash)) THEN
  RAISE EXCEPTION 'SEMANTIC_REFERENCE_BINDING_INVALID' USING ERRCODE='23514';END IF;
 count_targets:=jsonb_array_length(plan->'candidates');
 IF count_targets NOT BETWEEN 1 AND 5000 OR jsonb_array_length(params->'candidates')<>count_targets OR jsonb_array_length(snapshot->'items')<>count_targets
  OR (SELECT count(*) FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=w.input_snapshot_id)<>count_targets
  OR (SELECT count(*) FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='TransferCandidate')<>count_targets
  OR (SELECT count(DISTINCT c->'target'->>'asset_version_id') FROM jsonb_array_elements(plan->'candidates') c)<>count_targets
  OR (SELECT count(DISTINCT ref->>'artifact_id') FROM jsonb_array_elements(params->'candidates') ref)<>count_targets THEN
  RAISE EXCEPTION 'SEMANTIC_TARGET_COVERAGE_INVALID' USING ERRCODE='23514';END IF;
 IF EXISTS(WITH candidates AS MATERIALIZED(
   SELECT d->>'artifact_id' AS artifact_id,d->>'sha256' AS sha256,(d->>'body')::jsonb AS body
   FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='TransferCandidate'),
  refs AS MATERIALIZED(SELECT ref->>'artifact_id' AS artifact_id,ref->>'sha256' AS sha256 FROM jsonb_array_elements(params->'candidates') ref)
  SELECT 1 FROM jsonb_array_elements(plan->'candidates') WITH ORDINALITY c(body,position)
  LEFT JOIN candidates d ON d.body=c.body LEFT JOIN refs ref ON ref.artifact_id=d.artifact_id AND ref.sha256=d.sha256
  LEFT JOIN photo.snapshot_items i ON i.tenant_id=p_tenant AND i.snapshot_id=w.input_snapshot_id AND i.ordinal=c.position-1
  WHERE c.body->>'state' NOT IN('proposed','unsupported','rejected') OR c.body->>'state' IS NULL
   OR c.body->'target' IS DISTINCT FROM snapshot->'items'->(c.position::integer-1)->'binding'
   OR i.id IS NULL OR i.asset_version_id::text IS DISTINCT FROM c.body->'target'->>'asset_version_id'
   OR c.body->'target'->'source' IS DISTINCT FROM jsonb_build_object('artifact_id',i.artifact_id::text,'sha256',i.sha256)
   OR d.artifact_id IS NULL OR ref.artifact_id IS NULL) THEN
  RAISE EXCEPTION 'SEMANTIC_TARGET_BINDING_INVALID' USING ERRCODE='23514';END IF;
 INSERT INTO photo.semantic_intents(tenant_id,project_id,workflow_id,reference_version_id,snapshot_id,intent_artifact_id,intent_sha)
 SELECT p_tenant,w.project_id,w.id,reference_id,w.input_snapshot_id,(d->>'artifact_id')::uuid,intent_sha
 FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='EditIntent'
 ON CONFLICT(tenant_id,workflow_id) DO NOTHING;
 SELECT * INTO existing FROM photo.semantic_intents WHERE tenant_id=p_tenant AND workflow_id=p_workflow FOR UPDATE;
 IF existing.project_id<>w.project_id OR existing.reference_version_id<>reference_id OR existing.snapshot_id<>w.input_snapshot_id
  OR existing.intent_sha<>intent_sha OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_documents) d
   WHERE d->>'contract'='EditIntent' AND d->>'artifact_id'=existing.intent_artifact_id::text) THEN
  RAISE EXCEPTION 'SEMANTIC_INTENT_CONFLICT' USING ERRCODE='23514';END IF;
 intent_id:=existing.id;
 SELECT count(*) INTO count_existing FROM photo.semantic_targets WHERE tenant_id=p_tenant AND semantic_targets.intent_id=existing.id;
 IF count_existing=0 THEN
  INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,candidate_artifact_id,state)
  SELECT p_tenant,w.project_id,existing.id,((d->>'body')::jsonb->'target'->>'asset_version_id')::uuid,(d->>'artifact_id')::uuid,(d->>'body')::jsonb->>'state'
  FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='TransferCandidate';
 ELSE
  IF count_existing<>count_targets OR EXISTS(WITH candidates AS MATERIALIZED(
    SELECT d->>'artifact_id' AS artifact_id,(d->>'body')::jsonb->'target'->>'asset_version_id' AS version_id
    FROM jsonb_array_elements(p_documents) d WHERE d->>'contract'='TransferCandidate')
   SELECT 1 FROM photo.semantic_targets t LEFT JOIN candidates d ON d.version_id=t.asset_version_id::text AND d.artifact_id=t.candidate_artifact_id::text
   WHERE t.tenant_id=p_tenant AND t.intent_id=existing.id AND d.artifact_id IS NULL) THEN
   RAISE EXCEPTION 'SEMANTIC_TARGET_CONFLICT' USING ERRCODE='23514';END IF;
 END IF;
 RETURN intent_id;
END $$;
ALTER FUNCTION photo.initialize_semantic_review(uuid,uuid,uuid,uuid,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.initialize_semantic_review(uuid,uuid,uuid,uuid,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.initialize_semantic_review(uuid,uuid,uuid,uuid,bigint,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
