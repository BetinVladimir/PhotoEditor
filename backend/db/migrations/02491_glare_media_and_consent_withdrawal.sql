-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.restoration_completed_preview_media_live(p_tenant uuid,p_preview uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;x record;o jsonb;a photo.artifacts%ROWTYPE;
 receipt jsonb;params jsonb;manifest jsonb;expected jsonb;seen text[];n integer:=0;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_preview AND kind='preview'
  AND status='completed' AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id)<>4 THEN RETURN false;END IF;
 FOR x IN SELECT t.id,t.node_key,t.attempt,t.fencing_token,z.id result_id,z.outputs,
  d.v28_restoration_preview_params,d.v28_restoration_preview_derivative_receipt
 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id
  AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id
  AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 JOIN photo.artifacts pa ON pa.tenant_id=d.tenant_id AND pa.id=d.artifact_id AND pa.sha256=d.sha256
  AND pa.bytes=d.bytes AND pa.bucket=d.bucket AND pa.object_version=d.object_version
  AND pa.media_type='application/json' AND pa.state='ready' AND pa.verified_at IS NOT NULL
  AND pa.object_version NOT IN('','null') AND (pa.expires_at IS NULL OR pa.expires_at>clock_timestamp())
 WHERE t.tenant_id=p_tenant AND t.run_id=r.id AND t.operation='media.preview' AND t.operation_version='1'
  AND t.state='succeeded' AND NOT t.cancel_requested AND t.node_key IN(
   'restoration.before:preview1600','restoration.before:preview2560',
   'restoration.after:preview1600','restoration.after:preview2560')
 FOR SHARE OF t,z,d,pa LOOP
  n:=n+1;params:=x.v28_restoration_preview_params::jsonb;receipt:=x.v28_restoration_preview_derivative_receipt;
  expected:=CASE split_part(x.node_key,':',1) WHEN 'restoration.before' THEN r.config_snapshot->'before' ELSE r.config_snapshot->'after' END;
  IF expected IS NULL OR jsonb_typeof(expected) IS DISTINCT FROM 'object' OR receipt IS NULL OR receipt->>'result_id' IS DISTINCT FROM x.result_id::text
   OR receipt->>'attempt' IS DISTINCT FROM x.attempt::text OR receipt->>'fence' IS DISTINCT FROM x.fencing_token::text
   OR params->'image' IS DISTINCT FROM expected OR params->>'variant' IS DISTINCT FROM split_part(x.node_key,':',2)
   OR jsonb_typeof(receipt->'objects') IS DISTINCT FROM 'array' OR jsonb_array_length(receipt->'objects') IS DISTINCT FROM 2
   OR jsonb_typeof(x.outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(x.outputs) IS DISTINCT FROM 2
  THEN RETURN false;END IF;
  manifest:=(receipt->>'manifest')::jsonb;seen:='{}';
  IF manifest->'source' IS DISTINCT FROM expected OR manifest->'profile_hash' IS DISTINCT FROM params->'profile_hash'
   OR manifest->>'variant' IS DISTINCT FROM params->>'variant' THEN RETURN false;END IF;
  FOR o IN SELECT value FROM jsonb_array_elements(receipt->'objects') LOOP
   IF o->>'role' IS NULL OR o->>'role' NOT IN('derivative','derivative_manifest') OR o->>'role'=ANY(seen) THEN RETURN false;END IF;
   seen:=array_append(seen,o->>'role');
   SELECT artifact.* INTO a FROM photo.artifacts artifact JOIN photo.output_uploads u
    ON u.tenant_id=artifact.tenant_id AND u.task_id=x.id AND u.artifact_id=artifact.id
    AND u.attempt=x.attempt AND u.fence=x.fencing_token AND u.state='ready'
    AND u.role=artifact.output_role AND u.bucket=artifact.bucket AND u.object_key=artifact.object_key
    AND u.object_version=artifact.object_version AND u.sha256=artifact.sha256 AND u.bytes=artifact.bytes AND u.media_type=artifact.media_type
   WHERE artifact.tenant_id=p_tenant AND artifact.id=(o->>'artifact_id')::uuid AND artifact.sha256=o->>'sha256'
    AND artifact.bucket=o->>'bucket' AND artifact.object_key=o->>'key' AND artifact.object_version=o->>'version'
    AND to_jsonb(artifact.bytes)=o->'bytes' AND artifact.media_type=o->>'media_type' AND artifact.output_role=o->>'role'
    AND artifact.created_by_task_id=x.id AND artifact.produced_attempt=x.attempt AND artifact.produced_fence=x.fencing_token
    AND artifact.state='ready' AND artifact.verified_at IS NOT NULL AND artifact.object_version NOT IN('','null')
    AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp())
    AND x.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',artifact.id,'sha256',artifact.sha256))
   FOR SHARE OF artifact,u;
   IF NOT FOUND THEN RETURN false;END IF;
   IF a.output_role='derivative_manifest' THEN
    IF a.id::text IS DISTINCT FROM receipt->>'manifest_id' OR a.media_type<>'application/json'
     OR a.bytes NOT BETWEEN 2 AND 1048576 OR a.bytes IS DISTINCT FROM octet_length(receipt->>'manifest')::bigint
     OR a.sha256 IS DISTINCT FROM encode(sha256(convert_to(receipt->>'manifest','UTF8')),'hex') THEN RETURN false;END IF;
   ELSE
    IF a.media_type NOT IN('image/jpeg','image/webp') OR a.bytes NOT BETWEEN 2 AND 54525952
     OR manifest->'derivative' IS DISTINCT FROM jsonb_build_object('artifact_id',a.id,'sha256',a.sha256) THEN RETURN false;END IF;
   END IF;
  END LOOP;
 END LOOP;
 RETURN n=4;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.restoration_completed_preview_media_live(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.restoration_completed_preview_media_live(uuid,uuid) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_portal,photo_gateway,photo_maintenance;

DO $$ DECLARE name text;definition text;anchor text;BEGIN
 FOREACH name IN ARRAY ARRAY['photo.v28_restoration_review_decision_ready(uuid,uuid)','photo.v28_restoration_review_selection(uuid,uuid)'] LOOP
  definition:=pg_get_functiondef(name::regprocedure);anchor:='AND preview.kind=''preview''';
  IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'RESTORATION_LIVE_MEDIA_ANCHOR %',name;END IF;
  EXECUTE replace(definition,anchor,anchor||' AND photo.restoration_completed_preview_media_live(p_tenant,preview.id)');
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE photo.runs ADD COLUMN v28_glare_authority_failure jsonb
 CHECK(v28_glare_authority_failure IS NULL OR jsonb_typeof(v28_glare_authority_failure)='object');
-- +goose StatementBegin
CREATE FUNCTION photo.glare_withdrawn_quota_settled(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT FROM photo.runs r JOIN photo.runs origin ON origin.tenant_id=r.tenant_id
  AND (origin.id=r.id OR EXISTS(SELECT FROM photo.tasks ot WHERE ot.tenant_id=origin.tenant_id
   AND ot.run_id=origin.id AND ot.id=r.v28_restoration_preview_task_id))
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND origin.v28_glare_authority_failure IS NOT NULL
  AND origin.kind='v2' AND origin.status='cancelled' AND origin.cancel_requested
  AND r.status IN('cancelled','completed','completed_with_errors','failed')
  AND NOT EXISTS(SELECT FROM photo.runs family WHERE family.tenant_id=p_tenant
   AND (family.id=origin.id OR EXISTS(SELECT FROM photo.tasks ot WHERE ot.tenant_id=p_tenant AND ot.run_id=origin.id AND ot.id=family.v28_restoration_preview_task_id))
   AND (family.status NOT IN('cancelled','completed','completed_with_errors','failed')
    OR EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=family.id
     AND (t.state NOT IN('succeeded','failed','cancelled','skipped') OR t.lease_until IS NOT NULL))
    OR EXISTS(SELECT FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
     WHERE t.tenant_id=p_tenant AND t.run_id=family.id AND u.state NOT IN('ready','cleaned'))
    OR EXISTS(SELECT FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=family.id AND d.state NOT IN('ready','cleaned'))
    OR EXISTS(SELECT FROM photo.resource_reservations x JOIN photo.tasks t ON t.tenant_id=x.tenant_id AND t.id=x.task_id
     WHERE t.tenant_id=p_tenant AND t.run_id=family.id AND x.state<>'released')
    OR EXISTS(SELECT FROM photo.resource_slots x JOIN photo.tasks t ON t.tenant_id=x.tenant_id AND t.id=x.task_id
     WHERE t.tenant_id=p_tenant AND t.run_id=family.id))));
$$;
ALTER FUNCTION photo.glare_withdrawn_quota_settled(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.glare_withdrawn_quota_settled(uuid,uuid) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_portal,photo_gateway,photo_maintenance;

CREATE FUNCTION photo.release_next_glare_withdrawn_quota() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;
BEGIN
 SELECT rr.tenant_id,rr.id INTO candidate FROM photo.runs rr JOIN photo.quota_reservations reservation
  ON reservation.tenant_id=rr.tenant_id AND reservation.holder_type='run' AND reservation.holder_id=rr.id
 WHERE reservation.state='reserved' AND photo.glare_withdrawn_quota_settled(rr.tenant_id,rr.id)
 ORDER BY rr.created_at,rr.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=candidate.tenant_id FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT photo.glare_withdrawn_quota_settled(r.tenant_id,r.id) THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=r.tenant_id AND holder_type='run' AND holder_id=r.id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.principal_type<>'user' OR q.principal_id<>r.created_by OR q.files<>0 OR q.bytes<0 THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=r.tenant_id AND reserved_bytes>=q.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'GLARE_WITHDRAWN_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
 UPDATE photo.quota_reservations SET state='released',bytes=0,expires_at=clock_timestamp() WHERE tenant_id=r.tenant_id AND id=q.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.release_next_glare_withdrawn_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_next_glare_withdrawn_quota() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.release_next_glare_withdrawn_quota() TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.guard_glare_authority_failure() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$ BEGIN
 IF (TG_OP='INSERT' AND NEW.v28_glare_authority_failure IS NOT NULL) OR
 (TG_OP='UPDATE' AND NEW.v28_glare_authority_failure IS DISTINCT FROM OLD.v28_glare_authority_failure
 AND (current_user<>'photo_fn_owner' OR OLD.v28_glare_authority_failure IS NOT NULL)) THEN
 RAISE EXCEPTION 'GLARE_AUTHORITY_FAILURE_IMMUTABLE' USING ERRCODE='42501';END IF;RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_glare_authority_failure() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_glare_authority_failure() FROM PUBLIC;
CREATE TRIGGER runs_glare_authority_failure_guard BEFORE INSERT OR UPDATE OF v28_glare_authority_failure
 ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_glare_authority_failure();

-- No error-code guessing: only authenticated original proof plus an explicit
-- persisted revocation/epoch change is a permanent failure witness.
CREATE FUNCTION photo.completed_glare_consent_loss(p_tenant uuid,p_task uuid,p_result uuid)
RETURNS text LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;d photo.backend_documents%ROWTYPE;
 body jsonb;c jsonb;proof jsonb;proof_text text;project_id uuid;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='portrait.glare'
  AND operation_version='1' AND state='succeeded' AND canonical_result_id=p_result;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=p_result
  AND z.task_id=p_task AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token AND z.review_required) THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id AND kind='v2' AND graph_sealed
  AND status IN('running','waiting_review') AND NOT cancel_requested AND v28_restoration_descriptor_jcs IS NOT NULL;
 IF NOT FOUND THEN RETURN NULL;END IF;
 body:=r.v28_restoration_descriptor_jcs::jsonb->'params';
 IF r.v28_restoration_descriptor_jcs::jsonb->'task'->>'id' IS DISTINCT FROM t.id::text
  OR r.v28_restoration_descriptor_jcs::jsonb->'task'->>'params_artifact_id' IS DISTINCT FROM t.params_artifact_id::text
  OR r.v28_restoration_descriptor_jcs::jsonb->'task'->>'input_digest' IS DISTINCT FROM t.input_digest
  OR coalesce(body->>'mode','') NOT IN('generative','donor') THEN RETURN NULL;END IF;
 SELECT w.project_id INTO project_id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=r.id
  AND w.workflow_type='preview' AND w.renderer_version='v2.2' AND w.input_snapshot_id=r.input_snapshot_id;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT doc.* INTO d FROM photo.backend_documents doc WHERE doc.tenant_id=p_tenant AND doc.run_id=r.id
  AND doc.owner_id=t.id AND doc.artifact_id=(body->'confirmation'->>'artifact_id')::uuid
  AND doc.sha256=body->'confirmation'->>'sha256' AND doc.state='ready'
  AND doc.purpose=CASE body->>'mode' WHEN 'generative' THEN 'v28_glare_source_confirmation' ELSE 'v28_donor_confirmation' END;
 IF NOT FOUND THEN RETURN NULL;END IF;
 c:=CASE body->>'mode' WHEN 'generative' THEN d.v28_glare_source_issuer_context ELSE d.v28_donor_issuer_context END;
 proof_text:=c->>'proof_jcs';proof:=proof_text::jsonb;
 IF proof_text IS NULL OR octet_length(proof_text) NOT BETWEEN 2 AND 65536
  OR d.bytes IS DISTINCT FROM octet_length(proof_text)::bigint
  OR d.sha256 IS DISTINCT FROM encode(sha256(convert_to(proof_text,'UTF8')),'hex')
  OR c->>'tenant_id' IS DISTINCT FROM p_tenant::text OR c->>'run_id' IS DISTINCT FROM r.id::text
  OR c->>'project_id' IS DISTINCT FROM project_id::text OR c->>'actor_id' IS DISTINCT FROM r.created_by::text
  OR c->>'auth_session_id' IS DISTINCT FROM r.v28_restoration_session_id::text
  OR c->'params'->'confirmation'->>'artifact_id' IS DISTINCT FROM t.id::text
  OR c->'params'->'confirmation'->>'sha256' IS DISTINCT FROM d.sha256
  OR proof->>'schema_version' IS DISTINCT FROM '1' OR proof->>'actor_id' IS DISTINCT FROM r.created_by::text
  OR proof->'source' IS DISTINCT FROM body->'base' OR proof->'donor' IS DISTINCT FROM body->'donor'
  OR c->'params'->'base' IS DISTINCT FROM body->'base' OR c->'params'->'donor' IS DISTINCT FROM body->'donor'
  OR c->'params'->'source_faces' IS DISTINCT FROM body->'source_faces'
  OR c->'params'->'donor_faces' IS DISTINCT FROM body->'donor_faces'
  OR (CASE body->>'mode' WHEN 'generative' THEN c->'params'->'glasses_mask' ELSE c->'params'->'eyes_mask' END) IS DISTINCT FROM body->'glasses_mask'
  OR c->'params'->'protected' IS DISTINCT FROM body->'protected' OR c->'params'->'strength' IS DISTINCT FROM body->'strength'
 THEN RETURN NULL;END IF;
 IF body->>'mode'='generative' AND ((c->'params')-'confirmation') IS DISTINCT FROM (body-'confirmation') THEN RETURN NULL;END IF;
 IF EXISTS(SELECT FROM photo.project_persons p JOIN photo.consent_grants g
  ON g.tenant_id=p.tenant_id AND g.project_id=p.project_id AND g.person_id=p.id
  WHERE p.tenant_id=p_tenant AND p.project_id=project_id AND p.id=(proof->>'person_id')::uuid
   AND g.id=(proof->>'consent_id')::uuid AND g.purpose='identity_matching'
   AND (p.state='revoked' OR g.status='revoked' OR p.consent_epoch::text IS DISTINCT FROM proof->>'epoch'
    OR g.epoch::text IS DISTINCT FROM proof->>'epoch')) THEN RETURN 'GLARE_CONSENT_REVOKED';END IF;
 IF EXISTS(SELECT FROM photo.person_observations o WHERE o.tenant_id=p_tenant AND o.project_id=project_id
  AND o.person_id=(proof->>'person_id')::uuid AND o.id IN((proof->>'source_observation_id')::uuid,(proof->>'donor_observation_id')::uuid)
  AND o.confirmed_by IS NULL) THEN RETURN 'GLARE_SOURCE_CONFIRMATION_WITHDRAWN';END IF;
 RETURN NULL;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.completed_glare_consent_loss(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.completed_glare_consent_loss(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_portal,photo_gateway,photo_maintenance;

CREATE FUNCTION photo.withdraw_completed_glare_consent() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;child photo.runs%ROWTYPE;code text;proof jsonb;cursor_value bigint;
BEGIN
 SELECT t.tenant_id,t.run_id,t.id,t.canonical_result_id INTO candidate FROM photo.tasks t JOIN photo.runs r
  ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.operation='portrait.glare' AND t.state='succeeded'
  AND r.status IN('running','waiting_review') AND NOT r.cancel_requested AND r.v28_glare_authority_failure IS NULL
  AND photo.completed_glare_consent_loss(t.tenant_id,t.id,t.canonical_result_id) IS NOT NULL ORDER BY r.created_at,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=candidate.tenant_id FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF r.cancel_requested OR r.status NOT IN('running','waiting_review') OR r.v28_glare_authority_failure IS NOT NULL
  OR t.canonical_result_id IS DISTINCT FROM candidate.canonical_result_id THEN RETURN false;END IF;
 -- Fence the original proof and all identity rows before repeating the typed predicate.
 PERFORM d.id FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.owner_id=t.id ORDER BY d.id FOR SHARE;
 SELECT ((CASE (r.v28_restoration_descriptor_jcs::jsonb)->'params'->>'mode' WHEN 'generative'
  THEN d.v28_glare_source_issuer_context ELSE d.v28_donor_issuer_context END)->>'proof_jcs')::jsonb INTO proof
 FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.owner_id=t.id
 AND d.artifact_id=((r.v28_restoration_descriptor_jcs::jsonb)->'params'->'confirmation'->>'artifact_id')::uuid;
 PERFORM p.id FROM photo.project_persons p WHERE p.tenant_id=r.tenant_id AND p.id=(proof->>'person_id')::uuid FOR SHARE;
 PERFORM g.id FROM photo.consent_grants g WHERE g.tenant_id=r.tenant_id AND g.id=(proof->>'consent_id')::uuid FOR SHARE;
 PERFORM o.id FROM photo.person_observations o WHERE o.tenant_id=r.tenant_id
  AND o.id IN((proof->>'source_observation_id')::uuid,(proof->>'donor_observation_id')::uuid) ORDER BY o.id FOR SHARE;
 code:=photo.completed_glare_consent_loss(r.tenant_id,t.id,t.canonical_result_id);IF code IS NULL THEN RETURN false;END IF;
 UPDATE photo.runs SET v28_glare_authority_failure=jsonb_build_object('code',code,'task_id',t.id,'result_id',t.canonical_result_id,
  'attempt',t.attempt,'fence',t.fencing_token),errors=errors||jsonb_build_array(jsonb_build_object('code',code,
  'message','The original consent or confirmed identity was withdrawn. Start a new request after authorization is restored.'))
 WHERE tenant_id=r.tenant_id AND id=r.id;
 FOR child IN SELECT * FROM photo.runs rr WHERE rr.tenant_id=r.tenant_id AND
  (rr.id=r.id OR rr.v28_restoration_preview_task_id=t.id) ORDER BY rr.id FOR UPDATE LOOP
  IF child.status IN('completed','completed_with_errors','failed','cancelled') THEN CONTINUE;END IF;
  IF child.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
  UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1 WHERE tenant_id=child.tenant_id AND id=child.id;
  INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(r.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
  UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=r.tenant_id AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
  IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
  INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(r.tenant_id,cursor_value,child.id,child.created_by,'principal','run.cancelling',jsonb_build_object('run_id',child.id,'status','cancelling','revision',child.revision+1));
  INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
   VALUES(r.tenant_id,photo.v30_uuid5(child.id,'glare:consent:withdrawal'),'photo.events.v1','run.cancelling',
    jsonb_build_object('schema_version',1,'event_id',photo.v30_uuid5(child.id,'glare:consent:withdrawal'),
     'type','run.cancelling','tenant_id',r.tenant_id,'run_id',child.id,'status','cancelling','code',code));
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.withdraw_completed_glare_consent() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.withdraw_completed_glare_consent() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.withdraw_completed_glare_consent() TO photo_scheduler;

-- The existing review trigger accepted only decisions and otherwise blocked
-- ordinary run cancellation. Permit exactly the owner's pending→cancelled CAS.
DO $$ DECLARE definition text;anchor text;BEGIN
 definition:=pg_get_functiondef('photo.guard_v28_restoration_review()'::regprocedure);anchor:=' IF TG_OP=''INSERT'' THEN';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'RESTORATION_CANCEL_GUARD_DRIFT';END IF;
 EXECUTE replace(definition,anchor,$a$ IF TG_OP='UPDATE' AND OLD.kind IN('portrait.glare','image.reflection') AND OLD.state='pending' AND NEW.state='cancelled' THEN
  IF current_user<>'photo_fn_owner' OR NEW.revision<>OLD.revision+1
   OR (to_jsonb(NEW)-'state'-'revision') IS DISTINCT FROM (to_jsonb(OLD)-'state'-'revision')
   OR NOT EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=OLD.tenant_id AND r.id=OLD.run_id AND r.cancel_requested AND r.status='cancelling')
  THEN RAISE EXCEPTION 'RESTORATION_CANCEL_AUTHORITY' USING ERRCODE='42501';END IF;RETURN NEW;
 END IF;
$a$||anchor);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '02491 is forward-only';END $$;
-- +goose StatementEnd
