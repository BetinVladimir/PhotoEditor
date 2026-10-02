-- Persist an evaluated learning version only from adopted fit/evaluate results.
-- The scheduler validates exact canonical manifest/Evaluation bytes and SHAs
-- before invoking this fenced metadata transaction.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v23_learning_version(p_tenant uuid,p_run uuid,
 p_manifest jsonb,p_evaluation jsonb,p_evaluation_artifact uuid,p_evaluation_sha text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 p photo.learning_profiles%ROWTYPE; fit photo.tasks%ROWTYPE;
 evaluated photo.tasks%ROWTYPE; e photo.graph_expansions%ROWTYPE;
 z photo.task_results%ROWTYPE; a photo.artifacts%ROWTYPE;
 manifest_id uuid; manifest_sha text; candidate_id uuid; candidate_sha text;
 version_id uuid; prior photo.learning_versions%ROWTYPE;
 h bytea; next_version bigint; eligibility text; reason text;
 item record; grant_row photo.consent_grants%ROWTYPE; event_row photo.correction_events%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_evaluation_artifact IS NULL OR
  coalesce(p_evaluation_sha,'') !~ '^[0-9a-f]{64}$' OR
  jsonb_typeof(p_manifest) IS DISTINCT FROM 'object' OR
  jsonb_typeof(p_manifest->'examples') IS DISTINCT FROM 'array' OR
  jsonb_array_length(p_manifest->'examples') NOT BETWEEN 40 AND 5000 OR
  jsonb_typeof(p_manifest->'consents') IS DISTINCT FROM 'array' OR
  jsonb_typeof(p_evaluation) IS DISTINCT FROM 'object' OR
  p_evaluation->>'safety_status' IS NULL OR
  p_evaluation->>'safety_status' NOT IN ('pass','fail','not_measurable') OR
  jsonb_typeof(p_evaluation->'metric_eligible') IS DISTINCT FROM 'boolean' OR
  jsonb_typeof(p_evaluation->'eligible') IS DISTINCT FROM 'boolean' OR
  jsonb_typeof(p_evaluation->'safety_regression') IS DISTINCT FROM 'boolean' OR
  (p_evaluation->>'eligible') IS DISTINCT FROM
   (CASE WHEN (p_evaluation->>'metric_eligible')='true' AND
    (p_evaluation->>'safety_status')='pass' THEN 'true' ELSE 'false' END) OR
  (p_evaluation->>'safety_status'='fail') IS DISTINCT FROM
   (p_evaluation->>'safety_regression'='true')
  THEN RETURN NULL; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant
  AND x.id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.workflow_type='learning';
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR
  NOT r.graph_sealed OR r.cancel_requested OR w.id IS NULL OR
  w.renderer_version<>'v2.2' OR w.input_snapshot_id<>r.input_snapshot_id
  THEN RETURN NULL; END IF;
 SELECT * INTO p FROM photo.learning_profiles x WHERE x.tenant_id=p_tenant
  AND x.project_id=w.project_id AND
   x.id=(r.config_snapshot->'v23_training'->>'profile_id')::uuid FOR UPDATE;
 IF p.id IS NULL OR p.revision::text IS DISTINCT FROM
  r.config_snapshot->'v23_training'->>'expected_revision' OR
  p.id::text IS DISTINCT FROM p_manifest->>'profile_id' OR
  p.mode IS DISTINCT FROM p_manifest->>'mode' OR
  p.person_id::text IS DISTINCT FROM p_manifest->>'person_id'
  THEN RETURN NULL; END IF;
 SELECT * INTO e FROM photo.graph_expansions x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.kind='v2.learning.evaluate/1' AND
   x.state='closed';
 IF e.id IS NULL THEN RETURN NULL; END IF;
 SELECT * INTO fit FROM photo.tasks x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.id=e.parent_task_id FOR SHARE;
 SELECT * INTO evaluated FROM photo.tasks x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.node_key='learning.evaluate:0' FOR SHARE;
 IF fit.id IS NULL OR fit.operation<>'learning.fit' OR
  fit.state<>'succeeded' OR fit.canonical_result_id IS NULL OR
  evaluated.id IS NULL OR evaluated.operation<>'learning.evaluate' OR
  evaluated.state<>'succeeded' OR evaluated.canonical_result_id IS NULL OR
  NOT EXISTS(SELECT 1 FROM photo.task_edges edge WHERE edge.tenant_id=p_tenant
   AND edge.run_id=p_run AND edge.prerequisite_id=fit.id AND
    edge.dependent_id=evaluated.id AND edge.condition='success') OR
  NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
   AND i.consumer='v2-adopt' AND
    i.event_id=photo.v2_adoption_event_id(fit.id,fit.canonical_result_id)) OR
  NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
   AND i.consumer='v2-adopt' AND
    i.event_id=photo.v2_adoption_event_id(evaluated.id,evaluated.canonical_result_id))
  THEN RETURN NULL; END IF;
 SELECT i.artifact_id,i.sha256 INTO manifest_id,manifest_sha
  FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant
   AND i.snapshot_id=r.input_snapshot_id AND i.ordinal=0;
 IF manifest_id IS NULL OR manifest_sha IS DISTINCT FROM
  r.config_snapshot->'v23_training'->>'manifest_sha' OR
  p_evaluation->>'manifest_sha' IS DISTINCT FROM manifest_sha
  THEN RETURN NULL; END IF;
 SELECT * INTO z FROM photo.task_results result WHERE result.tenant_id=p_tenant
  AND result.id=fit.canonical_result_id AND result.task_id=fit.id AND
   result.attempt=fit.attempt AND result.fencing_token=fit.fencing_token;
 IF z.id IS NULL OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' OR
  jsonb_array_length(z.outputs)<>1 THEN RETURN NULL; END IF;
 candidate_id:=(z.outputs->0->>'artifact_id')::uuid;
 candidate_sha:=z.outputs->0->>'sha256';
 IF candidate_id IS NULL OR candidate_sha IS NULL OR
  candidate_sha IS DISTINCT FROM p_evaluation->>'candidate_sha' OR
  NOT EXISTS(SELECT 1 FROM photo.artifacts art WHERE art.tenant_id=p_tenant
   AND art.id=candidate_id AND art.sha256=candidate_sha AND art.state='ready'
   AND art.created_by_task_id=fit.id AND art.produced_attempt=fit.attempt
   AND art.produced_fence=fit.fencing_token AND art.output_role='document'
   AND art.media_type='application/json' AND art.verified_at IS NOT NULL
   AND (art.expires_at IS NULL OR art.expires_at>clock_timestamp()))
  THEN RETURN NULL; END IF;
 SELECT * INTO z FROM photo.task_results result WHERE result.tenant_id=p_tenant
  AND result.id=evaluated.canonical_result_id AND
   result.task_id=evaluated.id AND result.attempt=evaluated.attempt AND
   result.fencing_token=evaluated.fencing_token;
 IF z.id IS NULL OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' OR
  jsonb_array_length(z.outputs)<>1 OR
  z.outputs->0->>'artifact_id' IS DISTINCT FROM p_evaluation_artifact::text OR
  z.outputs->0->>'sha256' IS DISTINCT FROM p_evaluation_sha THEN RETURN NULL; END IF;
 SELECT * INTO a FROM photo.artifacts art WHERE art.tenant_id=p_tenant
  AND art.id=p_evaluation_artifact FOR SHARE;
 IF a.id IS NULL OR a.sha256<>p_evaluation_sha OR a.state<>'ready' OR
  a.output_role<>'document' OR a.media_type<>'application/json' OR
  a.verified_at IS NULL OR a.object_version IS NULL OR
  a.created_by_task_id<>evaluated.id OR
  a.produced_attempt<>evaluated.attempt OR
  a.produced_fence<>evaluated.fencing_token OR
  (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())
  THEN RETURN NULL; END IF;
 h:=public.digest(uuid_send(w.id)||convert_to('learning.version:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 version_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 SELECT * INTO prior FROM photo.learning_versions v WHERE v.tenant_id=p_tenant
  AND v.workflow_id=w.id;
 IF prior.id IS NOT NULL THEN
  IF prior.id=version_id AND prior.training_artifact_id=manifest_id AND
   prior.model_artifact_id=candidate_id AND
   prior.evaluation_artifact_id=p_evaluation_artifact AND
   EXISTS(SELECT 1 FROM photo.version_eligibility status
    WHERE status.tenant_id=p_tenant AND status.version_id=prior.id)
   THEN RETURN prior.id; END IF;
  RAISE EXCEPTION 'V23_VERSION_REPLAY_CONFLICT' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(p_manifest->'examples') x
  WHERE coalesce(x.value->>'event_id','') !~
   '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') OR
  (SELECT count(DISTINCT x.value->>'event_id') FROM
   jsonb_array_elements(p_manifest->'examples') x)<>
   jsonb_array_length(p_manifest->'examples') THEN RETURN NULL; END IF;
 FOR item IN SELECT value AS body FROM jsonb_array_elements(p_manifest->'examples') LOOP
  SELECT * INTO event_row FROM photo.correction_events c WHERE c.tenant_id=p_tenant
   AND c.project_id=w.project_id AND c.id=(item.body->>'event_id')::uuid
   AND c.learn AND c.consent_id IS NOT NULL AND c.consent_epoch IS NOT NULL;
  IF event_row.id IS NULL OR event_row.session_id::text IS DISTINCT FROM
   item.body->>'session_id' OR event_row.asset_version_id::text IS DISTINCT FROM
   item.body->'render_case'->'binding'->>'asset_version_id' OR
   event_row.approved_document_id IS NULL OR
   NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_manifest->'consents') g
    WHERE g.value->>'consent_id'=event_row.consent_id::text AND
     g.value->>'epoch'=event_row.consent_epoch::text) THEN RETURN NULL; END IF;
  SELECT * INTO grant_row FROM photo.consent_grants g WHERE g.tenant_id=p_tenant
   AND g.project_id=w.project_id AND g.id=event_row.consent_id FOR SHARE;
  IF grant_row.id IS NULL OR grant_row.status<>'granted' OR
   grant_row.epoch<>event_row.consent_epoch OR
   grant_row.purpose<>(CASE WHEN p.person_id IS NULL THEN
    'learning_photographer' ELSE 'learning_person' END) OR
   grant_row.person_id IS DISTINCT FROM p.person_id THEN RETURN NULL; END IF;
 END LOOP;
 SELECT coalesce(max(v.version_no),0)+1 INTO next_version FROM
  photo.learning_versions v WHERE v.tenant_id=p_tenant AND v.profile_id=p.id;
 IF next_version>9007199254740991 THEN RETURN NULL; END IF;
 eligibility:=CASE WHEN p_evaluation->>'metric_eligible'='false' OR
  p_evaluation->>'safety_status'='fail' THEN 'rejected'
  WHEN p_evaluation->>'safety_status'='not_measurable' THEN 'needs_review'
  ELSE 'eligible' END;
 reason:=CASE eligibility WHEN 'eligible' THEN NULL
  WHEN 'needs_review' THEN 'SAFETY_NOT_MEASURABLE'
  ELSE 'METRIC_OR_SAFETY_REJECTED' END;
 INSERT INTO photo.learning_versions(id,tenant_id,project_id,profile_id,
  version_no,workflow_id,training_artifact_id,model_artifact_id,
  evaluation_artifact_id,parent_version_id)
 VALUES(version_id,p_tenant,w.project_id,p.id,next_version,w.id,manifest_id,
  candidate_id,p_evaluation_artifact,p.active_version_id);
 INSERT INTO photo.version_eligibility(tenant_id,project_id,version_id,
  status,reason) VALUES(p_tenant,w.project_id,version_id,eligibility,reason);
 INSERT INTO photo.learning_example_links(tenant_id,project_id,version_id,
  event_id,consent_id,consent_epoch)
 SELECT p_tenant,w.project_id,version_id,(x.value->>'event_id')::uuid,
  c.consent_id,c.consent_epoch FROM jsonb_array_elements(p_manifest->'examples') x
 JOIN photo.correction_events c ON c.tenant_id=p_tenant AND c.project_id=w.project_id
  AND c.id=(x.value->>'event_id')::uuid;
 RETURN version_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_v23_learning_version(uuid,uuid,jsonb,jsonb,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v23_learning_version(uuid,uuid,jsonb,jsonb,uuid,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v23_learning_version(uuid,uuid,jsonb,jsonb,uuid,text)
 TO photo_scheduler;
