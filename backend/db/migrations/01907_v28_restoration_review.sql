-- +goose Up
-- A review is published only from the immutable physical-adoption receipt.
-- The scheduler calls this in the same transaction as receipt adoption.
CREATE UNIQUE INDEX reviews_one_v28_restoration_task ON photo.reviews(tenant_id,task_id)
 WHERE kind IN('portrait.glare','image.reflection') AND task_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION photo.guard_v28_restoration_review() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.kind IN('portrait.glare','image.reflection') AND current_user<>'photo_fn_owner'
  THEN RAISE EXCEPTION 'V28 review requires verified receipt' USING ERRCODE='42501';END IF;
 ELSIF OLD.kind IN('portrait.glare','image.reflection') OR
  NEW.kind IN('portrait.glare','image.reflection') THEN
  RAISE EXCEPTION 'V28 review decision requires dedicated authority' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v28_restoration_review() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_restoration_review() FROM PUBLIC;
CREATE TRIGGER reviews_v28_restoration_guard BEFORE INSERT OR UPDATE ON photo.reviews
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v28_restoration_review();

-- +goose StatementBegin
CREATE FUNCTION photo.create_v28_restoration_review(p_tenant uuid,p_task uuid,p_result uuid)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 existing photo.reviews%ROWTYPE;candidate photo.artifacts%ROWTYPE;base photo.artifacts%ROWTYPE;
 receipt jsonb;body jsonb;review_id uuid;cursor_value bigint;reason text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result
  AND task_id=p_task FOR SHARE;
 receipt:=r.v28_restoration_adoption_receipt;body:=receipt->'evidence';
 IF r.id IS NULL OR t.id IS NULL OR z.id IS NULL OR r.kind<>'v2'
 OR r.status NOT IN('running','waiting_review') OR r.cancel_requested
 OR NOT r.graph_sealed OR t.operation NOT IN('portrait.glare','image.reflection')
 OR t.operation_version<>'1' OR t.state<>'succeeded' OR t.cancel_requested
 OR t.canonical_result_id IS DISTINCT FROM p_result OR z.attempt IS DISTINCT FROM t.attempt
 OR z.fencing_token IS DISTINCT FROM t.fencing_token OR NOT z.review_required
 OR receipt->>'task' IS DISTINCT FROM p_task::text
 OR receipt->>'result' IS DISTINCT FROM p_result::text
 OR receipt->>'attempt' IS DISTINCT FROM t.attempt::text
 OR receipt->>'fence' IS DISTINCT FROM t.fencing_token::text
 OR body->>'operation' IS DISTINCT FROM t.operation
 OR body->'scope'->>'tenant' IS DISTINCT FROM p_tenant::text
 OR body->'scope'->>'task' IS DISTINCT FROM p_task::text
 OR body->'scope'->>'result' IS DISTINCT FROM p_result::text
 OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
  AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(p_task,p_result))
 THEN RETURN NULL;END IF;
 SELECT * INTO candidate FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id::text=body->'candidate'->'image'->>'artifact_id'
  AND a.sha256=body->'candidate'->'image'->>'sha256'
  AND a.created_by_task_id=p_task AND a.produced_attempt=t.attempt
  AND a.produced_fence=t.fencing_token AND a.output_role='working_image'
  AND a.media_type='application/json' AND a.state='ready'
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO base FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id::text=(r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->'source'->>'artifact_id'
  AND a.sha256=(r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->'source'->>'sha256'
  AND a.media_type='application/json' AND a.state='ready'
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=base.id
   AND i.sha256=base.sha256 AND i.contract='WorkingImage') THEN RETURN NULL;END IF;
 SELECT * INTO existing FROM photo.reviews v WHERE v.tenant_id=p_tenant
  AND v.task_id=p_task FOR UPDATE;
 IF FOUND THEN
  IF existing.run_id=r.id AND existing.kind=t.operation
   AND existing.candidate_artifact_id=candidate.id
   AND existing.candidate_hash=candidate.sha256
   AND existing.base_artifact_id=base.id THEN RETURN existing.id;END IF;
  RETURN NULL;
 END IF;
 reason:=CASE WHEN t.operation='image.reflection' THEN 'reflection_model_proposal'
  WHEN (r.v28_restoration_descriptor_jcs::jsonb)->'params'->>'mode'='generative'
   THEN 'synthetic_glare_proposal' ELSE 'donor_review' END;
 INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,
  base_artifact_id,candidate_hash,kind,reasons)
 VALUES(p_tenant,r.id,p_task,candidate.id,base.id,candidate.sha256,t.operation,
  jsonb_build_array(reason)) RETURNING id INTO review_id;
 IF r.status='running' THEN
  IF r.revision>=9007199254740991 THEN
   RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
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
   'tenant_id',p_tenant,'run_id',r.id,'review_id',review_id,'kind',t.operation));
 RETURN review_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_v28_restoration_review(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v28_restoration_review(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v28_restoration_review(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
