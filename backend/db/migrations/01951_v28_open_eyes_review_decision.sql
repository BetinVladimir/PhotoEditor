-- +goose Up
-- A donor review decision rechecks current identity consent, source evidence,
-- qualification and the exact adopted candidate in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_open_eyes_review_decision_ready(p_tenant uuid,p_review uuid,
 p_actor uuid,p_session uuid) RETURNS boolean LANGUAGE plpgsql VOLATILE
 SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 z photo.task_results%ROWTYPE;w photo.v2_workflows%ROWTYPE;plan jsonb;body jsonb;
 pin jsonb;proof text;context_value jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL
 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review
  AND kind='portrait.open_eyes' AND state='pending' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=v.run_id
  AND status='waiting_review' AND kind='v2' AND graph_sealed AND NOT cancel_requested
  FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=v.task_id
  AND run_id=r.id AND operation='portrait.open_eyes' AND operation_version='1'
  AND state='succeeded' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant
  AND id=t.canonical_result_id AND task_id=t.id AND attempt=t.attempt
  AND fencing_token=t.fencing_token AND review_required FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id
  AND workflow_type='preview' AND renderer_version='v2.2'
  AND input_snapshot_id=r.input_snapshot_id FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session)
  IS DISTINCT FROM true THEN RETURN false;END IF;
 plan:=r.v28_donor_descriptor_jcs::jsonb;body:=plan->'params';
 pin:=plan->'task'->'execution_profile'->'donor_qualification';
 IF body IS NULL OR pin IS NULL OR plan->'task'->>'id' IS DISTINCT FROM t.id::text
 OR body->'base'->'source'->>'artifact_id' IS DISTINCT FROM v.base_artifact_id::text
 OR body->'base'->'source'->>'sha256' IS DISTINCT FROM
  (SELECT sha256 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=v.base_artifact_id)
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.contract='WorkingImage'
   AND i.artifact_id=v.base_artifact_id AND i.sha256=body->'base'->'source'->>'sha256')
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=v.candidate_artifact_id AND a.sha256=v.candidate_hash
  AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt
  AND a.produced_fence=t.fencing_token AND a.output_role='working_image'
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l
  WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id
   AND l.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
   AND l.artifact_id=v.candidate_artifact_id AND l.sha256=v.candidate_hash)
 OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
  AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 THEN RETURN false;END IF;
 SELECT d.v28_donor_issuer_context INTO context_value FROM photo.backend_documents d
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=r.id AND d.state='ready'
  AND d.purpose='v28_donor_confirmation'
  AND d.artifact_id=(body->'confirmation'->>'artifact_id')::uuid
  AND d.sha256=body->'confirmation'->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256
  AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF d,a;
 proof:=context_value->>'proof_jcs';
 IF proof IS NULL OR octet_length(proof) NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(proof,'UTF8')),'hex') IS DISTINCT FROM body->'confirmation'->>'sha256'
 OR context_value->>'project_id' IS DISTINCT FROM w.project_id::text
 OR context_value->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR ((context_value->'params')-'confirmation') IS DISTINCT FROM (body-'confirmation')
 OR photo.lock_v28_donor_session(p_tenant,w.project_id,
  (context_value->>'shoot_session_id')::uuid,p_actor,p_session,proof::jsonb,
  body->'source_faces',body->'donor_faces') IS DISTINCT FROM true
 OR photo.lock_v28_donor_qualification_objects(p_tenant,w.project_id,
  p_actor,p_session,pin,plan->'qualification_objects') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_open_eyes_review_decision_ready(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_open_eyes_review_decision_ready(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_open_eyes_review_decision_ready(uuid,uuid,uuid,uuid) TO photo_api;

-- Only the dedicated owner function may perform the decision update.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v28_open_eyes_review_update() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.kind='portrait.open_eyes' OR NEW.kind='portrait.open_eyes' THEN
  IF current_user<>'photo_fn_owner' OR OLD.kind IS DISTINCT FROM NEW.kind
   OR OLD.tenant_id IS DISTINCT FROM NEW.tenant_id OR OLD.id IS DISTINCT FROM NEW.id
   OR OLD.run_id IS DISTINCT FROM NEW.run_id OR OLD.task_id IS DISTINCT FROM NEW.task_id
   OR OLD.candidate_artifact_id IS DISTINCT FROM NEW.candidate_artifact_id
   OR OLD.candidate_hash IS DISTINCT FROM NEW.candidate_hash
   OR OLD.base_artifact_id IS DISTINCT FROM NEW.base_artifact_id
   OR OLD.state<>'pending' OR NEW.state<>'decided'
   OR NEW.revision<>OLD.revision+1 OR NEW.decided_by IS NULL
   OR NEW.decided_at IS NULL OR NEW.decision NOT IN('accept','reject','skip')
   OR NEW.decision_route IS DISTINCT FROM (CASE NEW.decision
    WHEN 'accept' THEN 'candidate' WHEN 'reject' THEN 'base' ELSE 'skip' END)
   OR NEW.decision_hash !~ '^[0-9a-f]{64}$' OR NEW.selection IS NULL
  THEN RAISE EXCEPTION 'V28 donor review decision requires dedicated authority'
   USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v28_open_eyes_review_update() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_open_eyes_review_update() FROM PUBLIC;
CREATE TRIGGER reviews_v28_open_eyes_update_guard BEFORE UPDATE ON photo.reviews
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v28_open_eyes_review_update();

-- +goose StatementBegin
CREATE FUNCTION photo.decide_v28_open_eyes_review(p_tenant uuid,p_review uuid,
 p_actor uuid,p_session uuid,p_expected_revision bigint,p_decision text,
 p_selection jsonb,p_hash text,p_route text) RETURNS boolean LANGUAGE plpgsql
 SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;base_sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_expected_revision IS NULL OR p_expected_revision NOT BETWEEN 1 AND 9007199254740990
 OR p_decision NOT IN('accept','reject','skip')
 OR p_route IS DISTINCT FROM (CASE p_decision WHEN 'accept' THEN 'candidate'
  WHEN 'reject' THEN 'base' ELSE 'skip' END)
 OR coalesce(p_hash,'') !~ '^[0-9a-f]{64}$' OR p_selection IS NULL
 THEN RETURN false;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review FOR UPDATE;
 IF NOT FOUND OR v.kind<>'portrait.open_eyes' OR v.state<>'pending'
 OR v.revision<>p_expected_revision THEN RETURN false;END IF;
 SELECT sha256 INTO base_sha FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id=v.base_artifact_id FOR SHARE;
 IF base_sha IS NULL OR p_selection IS DISTINCT FROM jsonb_build_object(
  'version','candidate_review_v1','candidate',jsonb_build_object(
   'artifact_id',v.candidate_artifact_id::text,'sha256',v.candidate_hash),
  'base',jsonb_build_object('artifact_id',v.base_artifact_id::text,'sha256',base_sha),
  'revision',v.revision+1) THEN RETURN false;END IF;
 IF p_hash IS DISTINCT FROM encode(sha256(convert_to(format(
  '{"actor_id":"%s","decision":"%s","kind":"%s","review_id":"%s","run_id":"%s","selection":{"base":{"artifact_id":"%s","sha256":"%s"},"candidate":{"artifact_id":"%s","sha256":"%s"},"revision":%s,"version":"candidate_review_v1"},"tenant_id":"%s","version":"candidate_decision_v1"}',
  p_actor,p_decision,v.kind,p_review,v.run_id,v.base_artifact_id,
  base_sha,v.candidate_artifact_id,v.candidate_hash,v.revision+1,p_tenant),'UTF8')),'hex')
 OR photo.v28_open_eyes_review_decision_ready(p_tenant,p_review,p_actor,p_session)
  IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.reviews SET state='decided',decision=p_decision,decided_by=p_actor,
  decided_at=clock_timestamp(),revision=v.revision+1,selection=p_selection,
  decision_hash=p_hash,decision_route=p_route
  WHERE tenant_id=p_tenant AND id=p_review AND state='pending'
   AND revision=p_expected_revision;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.decide_v28_open_eyes_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.decide_v28_open_eyes_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.decide_v28_open_eyes_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
