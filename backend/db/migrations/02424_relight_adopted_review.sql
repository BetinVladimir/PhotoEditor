-- +goose Up
-- An relight review is a consequence of physically verified adoption, not
-- of an arbitrary review insert or an unadopted worker result.
ALTER TABLE photo.reviews ADD COLUMN relight_params_jcs text;
ALTER TABLE photo.reviews ADD CONSTRAINT reviews_relight_params_binding CHECK (
 (kind='image.relight' AND relight_params_jcs IS NOT NULL
  AND octet_length(relight_params_jcs) BETWEEN 2 AND 65536)
 OR (kind<>'image.relight' AND relight_params_jcs IS NULL));
CREATE UNIQUE INDEX reviews_one_v28_relight_task ON photo.reviews(tenant_id,task_id)
 WHERE kind='image.relight' AND task_id IS NOT NULL;
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v28_relight_review_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.kind='image.relight' AND current_user<>'photo_fn_owner' THEN
  RAISE EXCEPTION 'V28 relight review requires adopted result' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v28_relight_review_insert() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_relight_review_insert() FROM PUBLIC;
CREATE TRIGGER reviews_v28_relight_insert_guard BEFORE INSERT ON photo.reviews
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v28_relight_review_insert();

-- +goose StatementBegin
CREATE FUNCTION photo.create_v28_relight_review(p_tenant uuid,p_task uuid,p_result uuid,p_body text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 candidate photo.artifacts%ROWTYPE;base photo.artifacts%ROWTYPE;
 existing photo.reviews%ROWTYPE;body jsonb;review_id uuid;cursor_value bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_task IS NULL OR p_result IS NULL THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result
  AND task_id=p_task FOR SHARE;
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.v2_task_inputs i
  ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
  WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
   AND i.task_id=p_task AND i.contract='Params'
   AND a.sha256=encode(sha256(convert_to(p_body,'UTF8')),'hex')
   AND a.state='ready' AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
  AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(p_task,p_result))
 THEN RETURN NULL;END IF;
 body:=p_body::jsonb;
 IF r.id IS NULL OR z.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('running','waiting_review')
 OR r.cancel_requested OR NOT r.graph_sealed OR body IS NULL
 OR t.operation<>'image.relight' OR t.operation_version<>'1'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.canonical_result_id IS DISTINCT FROM p_result
 OR z.attempt IS DISTINCT FROM t.attempt OR z.fencing_token IS DISTINCT FROM t.fencing_token
 OR NOT z.review_required
 OR (SELECT count(*) FROM photo.tasks other WHERE other.tenant_id=p_tenant AND other.run_id=r.id)<>1
 THEN RETURN NULL;END IF;
 SELECT * INTO base FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=(body->'base'->'source'->>'artifact_id')::uuid
  AND a.sha256=body->'base'->'source'->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
  AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=base.id
   AND i.sha256=base.sha256 AND i.contract='WorkingImage') THEN RETURN NULL;END IF;
 SELECT a.* INTO candidate FROM photo.v2_workflows w
 JOIN photo.v2_artifact_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id
  AND l.project_id=w.project_id
 JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id
  AND a.sha256=l.sha256
 WHERE w.tenant_id=p_tenant AND w.run_id=r.id
  AND l.purpose=encode(sha256(convert_to('tasks/'||p_task||'/$/working_image','UTF8')),'hex')
  AND a.created_by_task_id=p_task AND a.produced_attempt=t.attempt
  AND a.produced_fence=t.fencing_token AND a.output_role='working_image'
  AND a.media_type='application/json' AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 IF NOT FOUND OR candidate.id=base.id THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w
  JOIN photo.v2_artifact_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id
   AND l.project_id=w.project_id
  JOIN photo.artifacts root ON root.tenant_id=l.tenant_id AND root.id=l.artifact_id
   AND root.sha256=l.sha256
  WHERE w.tenant_id=p_tenant AND w.run_id=r.id AND l.ordinal=0
   AND l.purpose=encode(sha256(convert_to('tasks/'||p_task||'/$','UTF8')),'hex')
   AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id::text,'sha256',root.sha256))
   AND root.created_by_task_id=p_task AND root.produced_attempt=t.attempt
   AND root.produced_fence=t.fencing_token AND root.output_role='document'
   AND root.media_type='application/json' AND root.state='ready' AND root.verified_at IS NOT NULL
   AND root.object_version NOT IN('','null')
   AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 SELECT * INTO existing FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.task_id=p_task FOR UPDATE;
 IF FOUND THEN
  IF existing.run_id=r.id AND existing.kind='image.relight'
   AND existing.candidate_artifact_id=candidate.id AND existing.candidate_hash=candidate.sha256
   AND existing.base_artifact_id=base.id AND existing.relight_params_jcs=p_body THEN RETURN existing.id;END IF;
  RETURN NULL;
 END IF;
 INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,
  base_artifact_id,candidate_hash,kind,reasons,relight_params_jcs)
 VALUES(p_tenant,r.id,p_task,candidate.id,base.id,candidate.sha256,'image.relight',
  '["relative_depth_exposure"]'::jsonb,p_body) RETURNING id INTO review_id;
 IF r.status='running' THEN
  IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
  INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
   ON CONFLICT(tenant_id) DO NOTHING;
  UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
   WHERE tenant_id=p_tenant AND last_cursor<9007199254740991
   RETURNING last_cursor INTO cursor_value;
  IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
  UPDATE photo.runs SET status='waiting_review',revision=revision+1
   WHERE tenant_id=p_tenant AND id=r.id;
  INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
  VALUES(p_tenant,cursor_value,r.id,r.created_by,'principal','v2.run.waiting_review',
   jsonb_build_object('run_id',r.id,'status','waiting_review','review_id',review_id));
 END IF;
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,review_id,'photo.events.v1','review.created',
  jsonb_build_object('schema_version',1,'event_id',review_id,'type','review.created',
   'tenant_id',p_tenant,'run_id',r.id,'review_id',review_id,'kind','image.relight'));
 RETURN review_id;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_v28_relight_review(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v28_relight_review(uuid,uuid,uuid,text) FROM PUBLIC;
-- Private publication: callable only through dedicated adoption below.

-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text)
 RENAME TO adopt_v28_relight_pre_review;
REVOKE ALL ON FUNCTION photo.adopt_v28_relight_pre_review(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text)
 FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v28_relight(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_inventory jsonb,p_digest text,p_evidence text,p_body text,p_depth text,p_subject text,p_guard text,p_subject_root text,p_guard_root text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE adopted boolean;review_id uuid;previous_tenant text;
BEGIN
 adopted:=photo.adopt_v28_relight_pre_review(p_tenant,p_task,p_attempt,p_fence,p_result,
  p_inventory,p_digest,p_evidence,p_body,p_depth,p_subject,p_guard,p_subject_root,p_guard_root);
 IF NOT adopted THEN RETURN false;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 review_id:=photo.create_v28_relight_review(p_tenant,p_task,p_result,p_body);
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 IF review_id IS NULL THEN
  RAISE EXCEPTION 'RELIGHT_REVIEW_PUBLICATION_STALE' USING ERRCODE='23514';
 END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- Until the dedicated preview/decision gate is installed, generic decision
-- functions must not mutate a relight review through their owner privileges.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v28_relight_review_update() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.kind='image.relight' OR NEW.kind='image.relight' THEN
  RAISE EXCEPTION 'Relight decision requires dedicated authority' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v28_relight_review_update() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_relight_review_update() FROM PUBLIC;
CREATE TRIGGER reviews_v28_relight_update_guard BEFORE UPDATE ON photo.reviews
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v28_relight_review_update();
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
