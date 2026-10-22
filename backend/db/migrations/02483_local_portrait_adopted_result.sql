-- +goose Up
-- +goose StatementBegin
-- Original Params and the live pinned source/qualification remain authoritative
-- after worker completion. This accessor does not accept a new descriptor.
CREATE FUNCTION photo.v28_local_portrait_adoption_params(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(params_id uuid,params_sha text,params_jcs text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;wire text;digest text;previous text;allowed boolean;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.operation NOT IN('portrait.hair','portrait.clothing') OR t.operation_version<>'1'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt
 OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result
 OR t.resource_class<>'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=r.id FOR SHARE;
 IF NOT FOUND OR t.state<>'succeeded' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt
  OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=t.run_id FOR SHARE;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested
 OR r.status NOT IN('running','waiting_review','completed') OR w.workflow_type<>'preview'
 OR w.renderer_version<>'v2.2' OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR NOT EXISTS(SELECT FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=p_result
  AND z.task_id=t.id AND z.attempt=p_attempt AND z.fencing_token=p_fence)
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true
 OR photo.v28_local_qualification_pin_ready(p_tenant,r.id,t.id) IS DISTINCT FROM true THEN RETURN;END IF;
 wire:=r.config_snapshot->'v28_local_portrait'->'intent'->>'params_jcs';
 digest:=encode(sha256(convert_to(wire,'UTF8')),'hex');
 IF wire IS NULL OR octet_length(wire) NOT BETWEEN 2 AND 65536
 OR r.config_snapshot->'v28_local_portrait'->'intent'->>'task_id' IS DISTINCT FROM t.id::text
 OR r.config_snapshot->'v28_local_portrait'->'intent'->>'operation' IS DISTINCT FROM t.operation
 OR r.config_snapshot->'v28_local_portrait'->'intent'->>'params_sha' IS DISTINCT FROM digest
 OR NOT EXISTS(SELECT FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id AND d.sha256=a.sha256 AND d.object_version=a.object_version
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.contract='Params' AND i.artifact_id=t.params_artifact_id
   AND i.sha256=digest AND a.bytes=octet_length(wire) AND a.media_type='application/json'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND d.run_id=r.id AND d.owner_id=t.id AND d.purpose='task_params' AND d.state='ready'
   AND d.bytes=a.bytes AND d.bucket=a.bucket)
 THEN RETURN;END IF;
 previous:=current_setting('app.tenant_id',true);PERFORM set_config('app.tenant_id',p_tenant::text,true);
 allowed:=photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
  (r.config_snapshot->'v28_local_portrait'->>'auth_session_id')::uuid,(wire::jsonb)->'base');
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
 IF allowed IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT t.params_artifact_id,digest,wire;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 IF previous IS NOT NULL THEN PERFORM set_config('app.tenant_id',previous,true);END IF;
 RETURN;
END $$;
ALTER FUNCTION photo.v28_local_portrait_adoption_params(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_adoption_params(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v28_local_portrait_adoption_params(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE photo.runs ADD COLUMN v28_local_result_receipt jsonb;
CREATE FUNCTION photo.guard_v28_local_result_receipt() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF current_user<>'photo_fn_owner' THEN
  IF TG_OP='INSERT' THEN
   IF NEW.v28_local_result_receipt IS NOT NULL THEN RAISE EXCEPTION 'LOCAL_RESULT_PRIVATE' USING ERRCODE='42501';END IF;
  ELSIF NEW.v28_local_result_receipt IS DISTINCT FROM OLD.v28_local_result_receipt THEN
   RAISE EXCEPTION 'LOCAL_RESULT_PRIVATE' USING ERRCODE='42501';
  END IF;
 END IF;
 RETURN NEW;
END $$;
-- SECURITY INVOKER is intentional: observe the caller of the UPDATE.
ALTER FUNCTION photo.guard_v28_local_result_receipt() SECURITY INVOKER;
ALTER FUNCTION photo.guard_v28_local_result_receipt() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_local_result_receipt() FROM PUBLIC;
CREATE TRIGGER local_result_receipt_private BEFORE INSERT OR UPDATE OF v28_local_result_receipt ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v28_local_result_receipt();
CREATE FUNCTION photo.publish_v28_local_portrait_result(p_tenant uuid,p_task uuid,p_result uuid)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;z photo.task_results%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 original record;base photo.artifacts%ROWTYPE;candidate photo.artifacts%ROWTYPE;root photo.artifacts%ROWTYPE;
 review_id uuid;receipt jsonb;existing photo.reviews%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO original FROM photo.v28_local_portrait_adoption_params(p_tenant,p_task,t.attempt,t.fencing_token,p_result);
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task FOR SHARE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id FOR SHARE;
 IF NOT EXISTS(SELECT FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
  AND event_id=photo.v2_adoption_event_id(p_task,p_result)) THEN RETURN NULL;END IF;
 SELECT * INTO base FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id=((original.params_jcs::jsonb)->'base'->'source'->>'artifact_id')::uuid
  AND sha256=(original.params_jcs::jsonb)->'base'->'source'->>'sha256' FOR SHARE;
 SELECT a.* INTO candidate FROM photo.v2_artifact_links l JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
 WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id
  AND l.purpose=encode(sha256(convert_to('tasks/'||p_task||'/$/working_image','UTF8')),'hex')
  AND a.created_by_task_id=p_task AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
  AND a.output_role='working_image' AND a.media_type='application/json' AND a.state='ready'
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 SELECT a.* INTO root FROM photo.v2_artifact_links l JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
 WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id
  AND l.purpose=encode(sha256(convert_to('tasks/'||p_task||'/$','UTF8')),'hex') AND l.ordinal=0
  AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
  AND a.created_by_task_id=p_task AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
  AND a.output_role='document' AND a.media_type='application/json' AND a.state='ready'
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 IF base.id IS NULL OR candidate.id IS NULL OR root.id IS NULL
 OR z.review_required IS DISTINCT FROM (t.operation='portrait.clothing'
  AND ((original.params_jcs::jsonb)->>'strength')::numeric>0
  AND ((original.params_jcs::jsonb)->>'wrinkle_strength')::numeric>0) THEN RETURN NULL;END IF;
 IF z.review_required THEN
  SELECT * INTO existing FROM photo.reviews WHERE tenant_id=p_tenant AND task_id=p_task FOR UPDATE;
  IF FOUND THEN
   IF existing.run_id<>r.id OR existing.kind<>t.operation OR existing.base_artifact_id<>base.id
    OR existing.candidate_artifact_id<>candidate.id OR existing.candidate_hash<>candidate.sha256 THEN RETURN NULL;END IF;
   review_id:=existing.id;
  ELSE
   INSERT INTO photo.reviews(tenant_id,run_id,task_id,base_artifact_id,candidate_artifact_id,candidate_hash,kind,reasons)
    VALUES(p_tenant,r.id,p_task,base.id,candidate.id,candidate.sha256,t.operation,'["wrinkle_correction"]') RETURNING id INTO review_id;
   INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
    VALUES(p_tenant,review_id,'photo.events.v1','review.created',jsonb_build_object('schema_version',1,'event_id',review_id,
     'type','review.created','tenant_id',p_tenant,'run_id',r.id,'review_id',review_id,'kind',t.operation));
  END IF;
 END IF;
 receipt:=jsonb_build_object('schema_version',1,'task_id',p_task,'result_id',p_result,'attempt',t.attempt,'fence',t.fencing_token,
  'root',jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),
  'base',jsonb_build_object('artifact_id',base.id,'sha256',base.sha256),
  'candidate',jsonb_build_object('artifact_id',candidate.id,'sha256',candidate.sha256),'review_id',review_id);
 IF r.v28_local_result_receipt IS NOT NULL AND r.v28_local_result_receipt IS DISTINCT FROM receipt THEN RETURN NULL;END IF;
 UPDATE photo.runs SET v28_local_result_receipt=receipt WHERE tenant_id=p_tenant AND id=r.id;
 RETURN coalesce(review_id,p_task);
END $$;
ALTER FUNCTION photo.publish_v28_local_portrait_result(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.publish_v28_local_portrait_result(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.publish_v28_local_portrait_result(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
-- Shared live authority for both pending review and decided selection. The
-- immutable receipt is not itself permission: canonical result and the original
-- source/qualification/session are revalidated on every consumer operation.
CREATE FUNCTION photo.v28_local_portrait_result_ready(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;q jsonb;original record;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT FOUND OR r.v28_local_result_receipt IS NULL THEN RETURN false;END IF;
 q:=r.v28_local_result_receipt;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=(q->>'task_id')::uuid FOR SHARE;
 IF NOT FOUND OR t.canonical_result_id::text IS DISTINCT FROM q->>'result_id'
 OR t.attempt::text IS DISTINCT FROM q->>'attempt' OR t.fencing_token::text IS DISTINCT FROM q->>'fence'
 THEN RETURN false;END IF;
 SELECT * INTO original FROM photo.v28_local_portrait_adoption_params(p_tenant,t.id,t.attempt,t.fencing_token,t.canonical_result_id);
 IF NOT FOUND OR (original.params_jcs::jsonb)->'base'->'source' IS DISTINCT FROM q->'base'
 OR NOT EXISTS(SELECT FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
  AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.v2_artifact_links l JOIN photo.artifacts a
   ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
  WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id
  AND l.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
  AND a.id::text=q->'candidate'->>'artifact_id' AND a.sha256=q->'candidate'->>'sha256'
  AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.output_role='working_image' AND a.media_type='application/json'
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR NOT EXISTS(SELECT FROM photo.v2_artifact_links l JOIN photo.artifacts a
   ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
  JOIN photo.task_results z ON z.tenant_id=a.tenant_id AND z.id=t.canonical_result_id
  WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id
  AND l.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex') AND l.ordinal=0
  AND a.id::text=q->'root'->>'artifact_id' AND a.sha256=q->'root'->>'sha256'
  AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
  AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.output_role='document' AND a.media_type='application/json'
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_local_portrait_result_ready(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_result_ready(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_local_portrait_result_ready(uuid,uuid,uuid,uuid) TO photo_api;
CREATE FUNCTION photo.v28_local_portrait_review_decision_ready(p_tenant uuid,p_review uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;r photo.runs%ROWTYPE;q jsonb;
BEGIN
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review
  AND kind IN('portrait.hair','portrait.clothing') AND state='pending' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=v.run_id AND status='waiting_review' FOR SHARE;
 IF NOT FOUND OR photo.v28_local_portrait_result_ready(p_tenant,r.id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 q:=r.v28_local_result_receipt;
 RETURN q->>'review_id'=v.id::text AND q->>'task_id'=v.task_id::text
  AND q->'base'->>'artifact_id'=v.base_artifact_id::text
  AND q->'candidate'->>'artifact_id'=v.candidate_artifact_id::text
  AND q->'candidate'->>'sha256'=v.candidate_hash;
END $$;
ALTER FUNCTION photo.v28_local_portrait_review_decision_ready(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_review_decision_ready(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_local_portrait_review_decision_ready(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.decide_v28_local_portrait_review(p_tenant uuid,p_review uuid,
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
 IF NOT FOUND OR v.kind NOT IN('portrait.hair','portrait.clothing') OR v.state<>'pending'
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
 OR photo.v28_local_portrait_review_decision_ready(p_tenant,p_review,p_actor,p_session)
  IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.reviews SET state='decided',decision=p_decision,decided_by=p_actor,
  decided_at=clock_timestamp(),revision=v.revision+1,selection=p_selection,
  decision_hash=p_hash,decision_route=p_route
  WHERE tenant_id=p_tenant AND id=p_review AND state='pending'
   AND revision=p_expected_revision;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.decide_v28_local_portrait_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.decide_v28_local_portrait_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.decide_v28_local_portrait_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)
 TO photo_api;
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v28_local_portrait_review_update() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.kind IN('portrait.hair','portrait.clothing') OR NEW.kind IN('portrait.hair','portrait.clothing') THEN
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
  THEN RAISE EXCEPTION 'V28 local portrait review decision requires dedicated authority'
   USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v28_local_portrait_review_update() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_local_portrait_review_update() FROM PUBLIC;
CREATE TRIGGER reviews_v28_local_portrait_update_guard BEFORE UPDATE ON photo.reviews
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v28_local_portrait_review_update();

-- +goose StatementBegin
CREATE FUNCTION photo.get_v28_local_portrait_review_selection(p_tenant uuid,p_actor uuid,p_session uuid,p_review uuid)
RETURNS TABLE(review_id uuid,decision_hash text,route text,image_id uuid,image_sha text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT v.id,v.decision_hash,v.decision_route,
 CASE v.decision_route WHEN 'candidate' THEN c.id WHEN 'base' THEN b.id END,
 CASE v.decision_route WHEN 'candidate' THEN c.sha256 WHEN 'base' THEN b.sha256 END
 FROM photo.reviews v JOIN photo.runs r ON r.tenant_id=v.tenant_id AND r.id=v.run_id
 JOIN photo.artifacts c ON c.tenant_id=v.tenant_id AND c.id=v.candidate_artifact_id
 JOIN photo.artifacts b ON b.tenant_id=v.tenant_id AND b.id=v.base_artifact_id
 WHERE v.tenant_id=p_tenant AND v.id=p_review AND v.kind IN('portrait.hair','portrait.clothing')
 AND v.state='decided' AND v.revision=2 AND v.decided_by IS NOT NULL AND v.decided_at IS NOT NULL
 AND v.decision_hash ~ '^[0-9a-f]{64}$' AND v.candidate_hash=c.sha256
 AND ((v.decision='accept' AND v.decision_route='candidate') OR(v.decision='reject' AND v.decision_route='base') OR(v.decision='skip' AND v.decision_route='skip'))
 AND v.selection=jsonb_build_object('version','candidate_review_v1','candidate',jsonb_build_object('artifact_id',c.id::text,'sha256',c.sha256),
 'base',jsonb_build_object('artifact_id',b.id::text,'sha256',b.sha256),'revision',2)
 AND r.v28_local_result_receipt->>'review_id'=v.id::text
 AND r.v28_local_result_receipt->>'task_id'=v.task_id::text
 AND r.v28_local_result_receipt->'candidate'=jsonb_build_object('artifact_id',c.id::text,'sha256',c.sha256)
 AND r.v28_local_result_receipt->'base'=jsonb_build_object('artifact_id',b.id::text,'sha256',b.sha256)
 AND photo.v28_local_portrait_result_ready(p_tenant,r.id,p_actor,p_session)
$$;
ALTER FUNCTION photo.get_v28_local_portrait_review_selection(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v28_local_portrait_review_selection(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.get_v28_local_portrait_review_selection(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v28_local_portrait_version(p_tenant uuid,p_review uuid,p_actor uuid,p_session uuid,
 p_decision_hash text,p_base text,p_image text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;src photo.asset_versions%ROWTYPE;old photo.asset_versions%ROWTYPE;
 selected record;body jsonb;base jsonb;image jsonb;identity uuid;next_no bigint;target_project uuid;
BEGIN
 IF p_tenant IS NULL OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_decision_hash IS NULL OR p_decision_hash !~ '^[0-9a-f]{64}$'
 OR p_base IS NULL OR octet_length(p_base) NOT BETWEEN 2 AND 1048576
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review
  AND kind IN('portrait.hair','portrait.clothing') AND state='decided' AND revision=2;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT (r.config_snapshot->'v28_local_portrait'->'intent'->>'params_jcs')::jsonb INTO body FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=v.run_id;
 SELECT * INTO src FROM photo.asset_versions WHERE tenant_id=p_tenant
  AND id=(body->'base'->>'asset_version_id')::uuid AND review_state='accepted';
 IF NOT FOUND OR src.master_artifact_id IS DISTINCT FROM v.base_artifact_id THEN RETURN NULL;END IF;
 SELECT w.project_id INTO target_project FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=v.run_id;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=target_project
  AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 -- Serialize version numbering before the authority reader acquires asset locks.
 PERFORM a.id FROM photo.assets a WHERE a.tenant_id=p_tenant AND a.id=src.asset_id
  AND a.project_id=target_project AND a.deleted_at IS NULL AND a.upload_state='verified' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO selected FROM photo.get_v28_local_portrait_review_selection(p_tenant,p_actor,p_session,p_review);
 IF NOT FOUND OR selected.decision_hash IS DISTINCT FROM p_decision_hash THEN RETURN NULL;END IF;
 base:=p_base::jsonb;image:=p_image::jsonb;
 IF jsonb_typeof(base) IS DISTINCT FROM 'object' OR jsonb_typeof(image) IS DISTINCT FROM 'object'
 OR encode(sha256(convert_to(p_base,'UTF8')),'hex') IS DISTINCT FROM src.content_hash
 OR base->>'dtype' IS DISTINCT FROM 'float32' OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR (base-'pixels') IS DISTINCT FROM (image-'pixels') THEN RETURN NULL;END IF;
 IF selected.route='skip' THEN RETURN NULL;END IF;
 IF selected.route='base' THEN
  IF selected.image_id=src.master_artifact_id AND selected.image_sha=src.content_hash
   AND p_image=p_base THEN RETURN src.id;END IF;
  RETURN NULL;
 END IF;
 IF selected.route IS DISTINCT FROM 'candidate'
 OR selected.image_id IS DISTINCT FROM v.candidate_artifact_id
 OR encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM selected.image_sha
 OR image->'pixels'->>'artifact_id' IS NULL
 OR image->'pixels'->>'artifact_id' IS NOT DISTINCT FROM base->'pixels'->>'artifact_id'
 THEN RETURN NULL;END IF;
 -- Child references must belong to this exact adopted result, not merely be
 -- ready artifacts elsewhere in the tenant. These links are also GC roots.
 IF EXISTS(SELECT 1 FROM (VALUES
  (image->'pixels','$/working_image/pixels'),
  (image->'alpha','$/working_image/alpha')) required(ref,path)
  WHERE required.ref IS NOT NULL AND required.ref<>'null'::jsonb AND NOT EXISTS(
   SELECT 1 FROM photo.v2_artifact_links link JOIN photo.v2_workflows workflow
    ON workflow.tenant_id=link.tenant_id AND workflow.id=link.workflow_id
   WHERE link.tenant_id=p_tenant AND workflow.run_id=v.run_id AND link.project_id=target_project
    AND link.artifact_id=(required.ref->>'artifact_id')::uuid
    AND link.sha256=required.ref->>'sha256'
    AND link.purpose=encode(sha256(convert_to('tasks/'||v.task_id::text||'/'||required.path,'UTF8')),'hex')))
 THEN RETURN NULL;END IF;
 -- All retained image children must still be ready exact-version objects.
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(
  (image->'pixels'->>'artifact_id')::uuid,(image->'alpha'->>'artifact_id')::uuid) ORDER BY a.id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=(image->'pixels'->>'artifact_id')::uuid AND a.sha256=image->'pixels'->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR (image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb AND NOT EXISTS(
  SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=(image->'alpha'->>'artifact_id')::uuid AND a.sha256=image->'alpha'->>'sha256'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN NULL;END IF;
 identity:=photo.semantic_uuid_v5(p_review,'V28.LocalPortraitAppliedVersion');
 SELECT * INTO old FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=identity;
 IF FOUND THEN
  IF old.asset_id=src.asset_id AND old.parent_asset_version_id=src.id
   AND old.master_artifact_id=selected.image_id AND old.content_hash=selected.image_sha
   AND old.metadata=image AND old.created_by_run_id=v.run_id AND old.review_state='accepted'
   AND EXISTS(SELECT 1 FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity
    AND source_version_id=src.id AND role='local_portrait_source' AND ordinal=0) THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 IF photo.v28_local_portrait_result_ready(p_tenant,v.run_id,p_actor,p_session) IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 SELECT coalesce(max(version_no),0)::bigint+1 INTO next_no FROM photo.asset_versions
  WHERE tenant_id=p_tenant AND asset_id=src.asset_id;
 IF next_no>2147483647 THEN RETURN NULL;END IF;
 INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,parent_asset_version_id,
  created_by_run_id,content_hash,metadata,review_state,version_no)
 VALUES(identity,p_tenant,src.asset_id,selected.image_id,src.id,v.run_id,selected.image_sha,image,'accepted',next_no);
 INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal)
 VALUES(p_tenant,identity,src.id,'local_portrait_source',0);
 RETURN identity;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.apply_v28_local_portrait_version(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v28_local_portrait_version(uuid,uuid,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.apply_v28_local_portrait_version(uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v28_local_portrait_review_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.kind IN('portrait.hair','portrait.clothing') AND current_user<>'photo_fn_owner' THEN
  RAISE EXCEPTION 'V28 local portrait review requires adopted result publisher' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v28_local_portrait_review_insert() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_local_portrait_review_insert() FROM PUBLIC;
CREATE TRIGGER reviews_v28_local_portrait_insert_guard BEFORE INSERT ON photo.reviews
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v28_local_portrait_review_insert();
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.get_v28_local_portrait_result(p_tenant uuid,p_actor uuid,p_session uuid,p_run uuid)
RETURNS TABLE(run_id uuid,result_id uuid,operation text,base_id uuid,base_sha text,candidate_id uuid,candidate_sha text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.id,t.canonical_result_id,t.operation,(q->'base'->>'artifact_id')::uuid,q->'base'->>'sha256',
  (q->'candidate'->>'artifact_id')::uuid,q->'candidate'->>'sha256'
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 CROSS JOIN LATERAL (SELECT r.v28_local_result_receipt q) receipt
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.id=(q->>'task_id')::uuid AND t.run_id=r.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 WHERE r.tenant_id=p_tenant AND w.id=p_run AND q->'review_id'='null'::jsonb
 AND NOT z.review_required AND r.status='completed'
 AND photo.v28_local_portrait_result_ready(p_tenant,r.id,p_actor,p_session)
$$;
ALTER FUNCTION photo.get_v28_local_portrait_result(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v28_local_portrait_result(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.get_v28_local_portrait_result(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v28_local_portrait_auto_version(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,
 p_result uuid,p_candidate_sha text,p_base text,p_image text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;src photo.asset_versions%ROWTYPE;old photo.asset_versions%ROWTYPE;
 selected record;body jsonb;base jsonb;image jsonb;identity uuid;next_no bigint;target_project uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_result IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_candidate_sha IS NULL OR p_candidate_sha !~ '^[0-9a-f]{64}$'
 OR p_base IS NULL OR octet_length(p_base) NOT BETWEEN 2 AND 1048576
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 -- Inspect identifiers before taking project/asset locks; no authority is
 -- granted until the dedicated live reader is repeated under those locks.
 SELECT r.id AS run_id,(q->>'result_id')::uuid AS result_id,
  (q->'base'->>'artifact_id')::uuid AS base_id,
  (q->'candidate'->>'artifact_id')::uuid AS candidate_id,q->'candidate'->>'sha256' AS candidate_sha
 INTO selected FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 CROSS JOIN LATERAL(SELECT r.v28_local_result_receipt q) receipt
 WHERE w.tenant_id=p_tenant AND w.id=p_run;
 IF NOT FOUND OR selected.result_id IS DISTINCT FROM p_result OR selected.candidate_sha IS DISTINCT FROM p_candidate_sha THEN RETURN NULL;END IF;
 v.run_id:=selected.run_id;v.base_artifact_id:=selected.base_id;v.candidate_artifact_id:=selected.candidate_id;
 SELECT (r.v28_local_result_receipt->>'task_id')::uuid,
  (r.config_snapshot->'v28_local_portrait'->'intent'->>'params_jcs')::jsonb INTO v.task_id,body
 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=v.run_id;
 SELECT * INTO src FROM photo.asset_versions WHERE tenant_id=p_tenant
  AND id=(body->'base'->>'asset_version_id')::uuid AND review_state='accepted';
 IF NOT FOUND OR src.master_artifact_id IS DISTINCT FROM v.base_artifact_id THEN RETURN NULL;END IF;
 SELECT w.project_id INTO target_project FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=v.run_id;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=target_project
  AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 -- Serialize version numbering before the authority reader acquires asset locks.
 PERFORM a.id FROM photo.assets a WHERE a.tenant_id=p_tenant AND a.id=src.asset_id
  AND a.project_id=target_project AND a.deleted_at IS NULL AND a.upload_state='verified' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO selected FROM photo.get_v28_local_portrait_result(p_tenant,p_actor,p_session,p_run);
 IF NOT FOUND OR selected.run_id IS DISTINCT FROM v.run_id OR selected.base_id IS DISTINCT FROM v.base_artifact_id
 OR selected.candidate_id IS DISTINCT FROM v.candidate_artifact_id OR selected.result_id IS DISTINCT FROM p_result
 OR selected.candidate_sha IS DISTINCT FROM p_candidate_sha THEN RETURN NULL;END IF;
 base:=p_base::jsonb;image:=p_image::jsonb;
 IF jsonb_typeof(base) IS DISTINCT FROM 'object' OR jsonb_typeof(image) IS DISTINCT FROM 'object'
 OR encode(sha256(convert_to(p_base,'UTF8')),'hex') IS DISTINCT FROM src.content_hash
 OR base->>'dtype' IS DISTINCT FROM 'float32' OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR (base-'pixels') IS DISTINCT FROM (image-'pixels') THEN RETURN NULL;END IF;
 IF encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM selected.candidate_sha
 OR image->'pixels'->>'artifact_id' IS NULL THEN RETURN NULL;END IF;
 -- Child references must belong to this exact adopted result, not merely be
 -- ready artifacts elsewhere in the tenant. These links are also GC roots.
 IF EXISTS(SELECT 1 FROM (VALUES
  (image->'pixels','$/working_image/pixels'),
  (image->'alpha','$/working_image/alpha')) required(ref,path)
  WHERE required.ref IS NOT NULL AND required.ref<>'null'::jsonb AND NOT EXISTS(
   SELECT 1 FROM photo.v2_artifact_links link JOIN photo.v2_workflows workflow
    ON workflow.tenant_id=link.tenant_id AND workflow.id=link.workflow_id
   WHERE link.tenant_id=p_tenant AND workflow.run_id=v.run_id AND link.project_id=target_project
    AND link.artifact_id=(required.ref->>'artifact_id')::uuid
    AND link.sha256=required.ref->>'sha256'
    AND link.purpose=encode(sha256(convert_to('tasks/'||v.task_id::text||'/'||required.path,'UTF8')),'hex')))
 THEN RETURN NULL;END IF;
 -- All retained image children must still be ready exact-version objects.
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(
  (image->'pixels'->>'artifact_id')::uuid,(image->'alpha'->>'artifact_id')::uuid) ORDER BY a.id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=(image->'pixels'->>'artifact_id')::uuid AND a.sha256=image->'pixels'->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR (image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb AND NOT EXISTS(
  SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=(image->'alpha'->>'artifact_id')::uuid AND a.sha256=image->'alpha'->>'sha256'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN NULL;END IF;
 identity:=photo.semantic_uuid_v5(p_result,'V28.LocalPortraitAutoAppliedVersion');
 SELECT * INTO old FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=identity;
 IF FOUND THEN
  IF old.asset_id=src.asset_id AND old.parent_asset_version_id=src.id
   AND old.master_artifact_id=selected.candidate_id AND old.content_hash=selected.candidate_sha
   AND old.metadata=image AND old.created_by_run_id=v.run_id AND old.review_state='accepted'
   AND EXISTS(SELECT 1 FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity
    AND source_version_id=src.id AND role='local_portrait_source' AND ordinal=0) THEN RETURN identity;END IF;
  RETURN NULL;
 END IF;
 IF photo.v28_local_portrait_result_ready(p_tenant,v.run_id,p_actor,p_session) IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 SELECT coalesce(max(version_no),0)::bigint+1 INTO next_no FROM photo.asset_versions
  WHERE tenant_id=p_tenant AND asset_id=src.asset_id;
 IF next_no>2147483647 THEN RETURN NULL;END IF;
 INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,parent_asset_version_id,
  created_by_run_id,content_hash,metadata,review_state,version_no)
 VALUES(identity,p_tenant,src.asset_id,selected.candidate_id,src.id,v.run_id,selected.candidate_sha,image,'accepted',next_no);
 INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal)
 VALUES(p_tenant,identity,src.id,'local_portrait_source',0);
 RETURN identity;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.apply_v28_local_portrait_auto_version(uuid,uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v28_local_portrait_auto_version(uuid,uuid,uuid,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.apply_v28_local_portrait_auto_version(uuid,uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
