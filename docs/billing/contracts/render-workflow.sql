-- +goose Up
INSERT INTO photo.billing_operation_policies(id,operation_code,operation_version,charge_context,policy_version,billing_class,units_per_success,success_contract,rerun_policy,effective_from,policy_snapshot_hash)
VALUES('4c391b00-d7bb-5107-9222-f18400002605','render.compose','1','standard_root',1,'included_standard',0,'render_adopted_selection_v1','standard_window',now(),sha256(convert_to('{"included_reruns":2,"policy_version":1,"rerun_window_hours":168,"schema_version":1,"success_contract":"render_adopted_selection_v1"}','UTF8')));
CREATE TABLE photo.billing_render_controls(tenant_id uuid PRIMARY KEY REFERENCES photo.tenants(id),enabled boolean NOT NULL DEFAULT false,policy_version integer NOT NULL DEFAULT 1 CHECK(policy_version=1));
ALTER TABLE photo.billing_usage_requests DROP CONSTRAINT billing_usage_requests_route_kind_check;
ALTER TABLE photo.billing_usage_requests ADD CONSTRAINT billing_usage_requests_route_kind_check CHECK (
 (route='Run.Apply' AND workflow_kind='standard_apply') OR (route='SessionAnalysis.Apply' AND workflow_kind='session_analysis_apply') OR
 (route='Run.SaveEdits' AND workflow_kind='save_edits_style') OR (route='V2.Render' AND workflow_kind='v2_render'));
-- V2 selection snapshots have no collection item. Apply binders still require one.
ALTER TABLE photo.billing_usage_charge_items ALTER COLUMN source_item_id DROP NOT NULL;
ALTER TABLE photo.billing_usage_run_links DROP CONSTRAINT billing_usage_run_links_relation_check;
ALTER TABLE photo.billing_usage_run_links ADD CONSTRAINT billing_usage_run_links_relation_check CHECK(relation IN('root','technical_child'));
ALTER TABLE photo.billing_usage_requests ADD COLUMN render_reconcile_after timestamptz;
CREATE INDEX billing_render_reconcile_queue ON photo.billing_usage_requests(render_reconcile_after,created_at,id) WHERE route='V2.Render' AND status='accepted';
CREATE TABLE photo.billing_render_frames(
 tenant_id uuid NOT NULL,charge_item_id uuid NOT NULL,run_id uuid NOT NULL,task_id uuid NOT NULL,source_version_id uuid NOT NULL,ordinal bigint NOT NULL CHECK(ordinal>=0),
 PRIMARY KEY(tenant_id,task_id),
 FOREIGN KEY(tenant_id,charge_item_id,run_id) REFERENCES photo.billing_usage_run_links(tenant_id,charge_item_id,run_id),
 FOREIGN KEY(tenant_id,task_id) REFERENCES photo.tasks(tenant_id,id),FOREIGN KEY(tenant_id,source_version_id) REFERENCES photo.asset_versions(tenant_id,id));
CREATE INDEX billing_render_frames_by_charge ON photo.billing_render_frames(tenant_id,charge_item_id,ordinal);
CREATE INDEX billing_render_frames_by_run ON photo.billing_render_frames(tenant_id,run_id,task_id);
CREATE TABLE photo.billing_render_results(
 tenant_id uuid NOT NULL,charge_item_id uuid NOT NULL,task_id uuid NOT NULL,result_id uuid NOT NULL,root_artifact_id uuid NOT NULL,image_artifact_id uuid NOT NULL,adopted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,charge_item_id),FOREIGN KEY(tenant_id,task_id) REFERENCES photo.billing_render_frames(tenant_id,task_id),
 FOREIGN KEY(tenant_id,charge_item_id) REFERENCES photo.billing_usage_charge_items(tenant_id,id),
 FOREIGN KEY(tenant_id,result_id) REFERENCES photo.task_results(tenant_id,id),FOREIGN KEY(tenant_id,root_artifact_id) REFERENCES photo.artifacts(tenant_id,id),FOREIGN KEY(tenant_id,image_artifact_id) REFERENCES photo.artifacts(tenant_id,id));
DO $$ DECLARE tab text;BEGIN
 FOREACH tab IN ARRAY ARRAY['billing_render_controls','billing_render_frames','billing_render_results'] LOOP
 EXECUTE format('ALTER TABLE photo.%I ENABLE ROW LEVEL SECURITY',tab);
 EXECUTE format('ALTER TABLE photo.%I FORCE ROW LEVEL SECURITY',tab);
 EXECUTE format('CREATE POLICY billing_owner ON photo.%I TO photo_fn_owner USING (true) WITH CHECK (true)',tab);
 EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON photo.%I TO photo_fn_owner',tab);
 EXECUTE format('REVOKE ALL ON photo.%I FROM PUBLIC,photo_api,photo_scheduler,photo_worker',tab);
 END LOOP;
END $$;
CREATE FUNCTION photo.billing_prepare_render(p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expired_lease record;
BEGIN
 PERFORM 1 FROM photo.billing_render_controls WHERE tenant_id=p_tenant AND enabled FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID';END IF;
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID';END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 PERFORM photo.billing_quota_lock(p_tenant,'standard_photo');
 FOR expired_lease IN SELECT id,fence_token FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code='standard_photo' AND state='active' AND expires_at<=clock_timestamp() ORDER BY expires_at,id LIMIT 16 LOOP
  PERFORM photo.billing_close_admission_lease(p_tenant,'standard_photo',expired_lease.id,expired_lease.fence_token);
 END LOOP;
 IF NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID';END IF;
 RETURN true;
END $$;

CREATE FUNCTION photo.billing_render_source_frames(p_tenant uuid,p_root uuid)
RETURNS TABLE(run_id uuid,task_id uuid,asset_version_id uuid,ordinal bigint,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH root AS(SELECT * FROM photo.runs WHERE tenant_id=p_tenant AND id=p_root),
 children AS(
 SELECT c.*,coalesce((part.value->>'first_ordinal')::bigint,0) AS first_ordinal FROM root
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN root.kind='bulk' THEN root.config_snapshot->'parts' ELSE '[{}]'::jsonb END) part
 JOIN photo.runs c ON c.tenant_id=root.tenant_id AND c.created_by=root.created_by AND c.input_snapshot_id=root.input_snapshot_id
 AND ((root.kind='v2' AND c.id=root.id) OR (root.kind='bulk' AND c.id::text=part.value->>'run_id' AND c.config_hash=part.value->>'config_sha'
 AND c.config_snapshot->>'project_id'=root.config_snapshot->>'project_id'
 AND c.config_snapshot#>>'{v26_render,auth_session_id}'=root.config_snapshot->>'auth_session_id'))
 WHERE c.kind='v2')
 SELECT DISTINCT ON (t.id) c.id,t.id,si.asset_version_id,c.first_ordinal+n.ordinality-1,orig.sha256
 FROM children c CROSS JOIN LATERAL jsonb_array_elements(c.config_snapshot#>'{v26_render,nodes}') WITH ORDINALITY n
 JOIN photo.tasks t ON t.tenant_id=c.tenant_id AND t.run_id=c.id AND t.id::text=n.value->>'task_id'
  AND t.operation='render.compose' AND t.operation_version='1' AND t.params_artifact_id::text=n.value#>>'{params,artifact_id}' AND t.input_digest=n.value->>'input_digest'
 JOIN photo.snapshot_items si ON si.tenant_id=c.tenant_id AND si.snapshot_id=c.input_snapshot_id
  AND si.asset_version_id::text=((n.value->>'params_jcs')::jsonb)#>>'{scalars,source,asset_version_id}'
  AND si.artifact_id::text=((n.value->>'params_jcs')::jsonb)#>>'{scalars,source,source,artifact_id}'
  AND si.sha256=((n.value->>'params_jcs')::jsonb)#>>'{scalars,source,source,sha256}'
 JOIN photo.asset_versions av ON av.tenant_id=si.tenant_id AND av.id=si.asset_version_id AND av.master_artifact_id=si.artifact_id AND av.content_hash=si.sha256
 JOIN photo.assets a ON a.tenant_id=av.tenant_id AND a.id=av.asset_id AND a.upload_state='verified' AND a.deleted_at IS NULL AND a.project_id::text=c.config_snapshot->>'project_id'
 JOIN photo.artifacts orig ON orig.tenant_id=a.tenant_id AND orig.id=a.original_artifact_id AND orig.state='ready' AND orig.object_version IS NOT NULL AND (orig.expires_at IS NULL OR orig.expires_at>clock_timestamp())
 WHERE encode(sha256(convert_to(n.value->>'params_jcs','UTF8')),'hex')=n.value#>>'{params,sha256}' ORDER BY t.id,si.ordinal;
$$;
CREATE FUNCTION photo.billing_bind_render(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_instance text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; req photo.billing_usage_requests%ROWTYPE; rec record; ident photo.billing_photo_identities%ROWTYPE;
 slot_id uuid; item_id uuid; quote uuid; request_id uuid; period uuid; paid bigint:=0; available numeric; lease_id uuid; fence uuid;
 gen bigint; claim uuid; decision text; q bigint; n timestamptz; valid_count bigint; expected bigint;p_key text;p_hash bytea;frames jsonb;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT FOUND OR r.created_by<>p_actor OR r.kind NOT IN('bulk','v2') THEN RAISE EXCEPTION 'BILLING_RUN_INVALID';END IF;
 IF NOT photo.billing_prepare_render(p_tenant,p_actor,p_session,(r.config_snapshot->>'project_id')::uuid) THEN RETURN NULL;END IF;
 IF p_instance IS NULL OR length(p_instance) NOT BETWEEN 1 AND 120 THEN RAISE EXCEPTION 'BILLING_REQUEST_INVALID';END IF;
 p_key:=p_run::text;p_hash:=decode(r.config_hash,'hex');
 SELECT * INTO req FROM photo.billing_usage_requests WHERE tenant_id=p_tenant AND run_id=p_run;
 IF FOUND THEN
  IF req.route<>'V2.Render' OR req.canonical_request_hash<>p_hash THEN RAISE EXCEPTION 'BILLING_IDEMPOTENCY_CONFLICT';END IF;
  RETURN req.id;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.runs p CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(p.config_snapshot->'parts')='array' THEN p.config_snapshot->'parts' ELSE '[]'::jsonb END) part
 WHERE p.tenant_id=p_tenant AND p.kind='bulk' AND part->>'run_id'=p_run::text)
 OR EXISTS(SELECT 1 FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND final_run_id=p_run) THEN RAISE EXCEPTION 'BILLING_CHILD_NOT_ROOT';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id AND principal_type='user' AND principal_id=p_actor AND (expires_at IS NULL OR expires_at>clock_timestamp())) THEN RAISE EXCEPTION 'BILLING_SOURCE_INVALID';END IF;
 IF r.status<>'draft' OR r.graph_sealed OR r.cancel_requested THEN RAISE EXCEPTION 'BILLING_RUN_INVALID';END IF;
 n:=clock_timestamp();
 SELECT id INTO period FROM photo.billing_periods WHERE tenant_id=p_tenant AND state='open' AND starts_at<=n AND ends_at>n;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_PERIOD_INVALID'; END IF;
 SELECT CASE WHEN r.kind='bulk' THEN (SELECT sum((p->>'frame_count')::integer) FROM jsonb_array_elements(r.config_snapshot->'parts') p)
 ELSE jsonb_array_length(r.config_snapshot#>'{v26_render,nodes}') END INTO expected;
 SELECT coalesce(jsonb_agg(to_jsonb(f)),'[]'::jsonb) INTO frames FROM photo.billing_render_source_frames(p_tenant,p_run) f;
 valid_count:=jsonb_array_length(frames);
 IF r.kind='bulk' AND expected IS DISTINCT FROM (SELECT item_count FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id) THEN RAISE EXCEPTION 'BILLING_SOURCE_INVALID';END IF;
 IF expected NOT BETWEEN 1 AND 5000 OR valid_count<>expected THEN RAISE EXCEPTION 'BILLING_SOURCE_INVALID';END IF;
 IF EXISTS(SELECT 1 FROM jsonb_to_recordset(frames) AS f(run_id uuid,task_id uuid,asset_version_id uuid,ordinal bigint,sha256 text) JOIN photo.runs c ON c.tenant_id=p_tenant AND c.id=f.run_id JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=f.task_id
 WHERE c.status<>'draft' OR c.graph_sealed OR c.cancel_requested OR t.state<>'blocked') THEN RAISE EXCEPTION 'BILLING_RUN_INVALID';END IF;
 quote:=gen_random_uuid();request_id:=gen_random_uuid();
 -- The cost ceiling and all slot decisions are pinned before any task can execute.
 INSERT INTO photo.billing_cost_quotes(id,tenant_id,actor_id,request_hash,snapshot_id,policy_version,max_quantity,expires_at,state)
 VALUES(quote,p_tenant,p_actor,p_hash,r.input_snapshot_id,1,expected,n+interval '15 minutes','accepted');
 INSERT INTO photo.billing_usage_requests(id,tenant_id,actor_user_id,session_id,request_key,route,canonical_request_hash,cost_quote_id,run_id,period_id,workflow_kind)
 VALUES(request_id,p_tenant,p_actor,p_session,p_key,'V2.Render',p_hash,quote,p_run,period,'v2_render');
 FOR rec IN SELECT DISTINCT ON (f.sha256) f.sha256,f.ordinal,NULL::uuid AS item_id,f.asset_version_id FROM jsonb_to_recordset(frames) AS f(run_id uuid,task_id uuid,asset_version_id uuid,ordinal bigint,sha256 text) ORDER BY f.sha256,f.ordinal LOOP
  INSERT INTO photo.billing_photo_identities(tenant_id,source_digest) VALUES(p_tenant,decode(rec.sha256,'hex')) ON CONFLICT DO NOTHING;
  SELECT * INTO ident FROM photo.billing_photo_identities WHERE tenant_id=p_tenant AND source_digest=decode(rec.sha256,'hex') FOR UPDATE;
  slot_id:=NULL;decision:='paid';q:=1;
  IF ident.first_success_at IS NULL THEN
   IF EXISTS(SELECT 1 FROM photo.billing_usage_charge_items WHERE tenant_id=p_tenant AND photo_identity_id=ident.id AND charge_kind='initial' AND state='accepted') THEN RAISE EXCEPTION 'PHOTO_INITIAL_IN_PROGRESS'; END IF;
   decision:='initial';
  ELSIF ident.window_ends_at>n THEN
   SELECT id INTO slot_id FROM photo.billing_photo_free_slots WHERE tenant_id=p_tenant AND photo_identity_id=ident.id AND slot_kind IN ('rerun_1','rerun_2') AND state='available' ORDER BY slot_kind LIMIT 1 FOR UPDATE;
   IF FOUND THEN decision:='free';q:=0;END IF;
  END IF;
  INSERT INTO photo.billing_usage_charge_items(tenant_id,usage_request_id,ordinal,photo_identity_id,source_item_id,source_version_id,charge_kind,max_quantity,operation_policy_id)
  VALUES(p_tenant,request_id,rec.ordinal,ident.id,rec.item_id,rec.asset_version_id,decision,q,'4c391b00-d7bb-5107-9222-f18400002605') RETURNING id INTO item_id;
  IF decision='initial' THEN
   INSERT INTO photo.billing_photo_free_slots(tenant_id,photo_identity_id,slot_kind,state,usage_charge_item_id,held_at) VALUES(p_tenant,ident.id,'first_result','held',item_id,n) ON CONFLICT(tenant_id,photo_identity_id,slot_kind) DO UPDATE SET state='held',usage_charge_item_id=EXCLUDED.usage_charge_item_id,held_at=EXCLUDED.held_at;
  END IF;
  IF q=0 THEN
   UPDATE photo.billing_photo_free_slots SET state='held',usage_charge_item_id=item_id,held_at=n WHERE id=slot_id AND tenant_id=p_tenant;
   INSERT INTO photo.billing_usage_tickets(tenant_id,request_id,charge_item_id,quantity,free_slot_id) VALUES(p_tenant,request_id,item_id,0,slot_id);
  ELSE paid:=paid+1;END IF;
  INSERT INTO photo.billing_usage_run_links(tenant_id,charge_item_id,run_id,relation) VALUES(p_tenant,item_id,p_run,'root');
 END LOOP;
 -- Resolve source inventory once; link every frame in set operations, not one source scan per photo.
 INSERT INTO photo.billing_usage_run_links(tenant_id,charge_item_id,run_id,relation)
 SELECT DISTINCT p_tenant,ci.id,f.run_id,'technical_child'
 FROM jsonb_to_recordset(frames) AS f(run_id uuid,task_id uuid,asset_version_id uuid,ordinal bigint,sha256 text)
 JOIN photo.billing_photo_identities photo_key ON photo_key.tenant_id=p_tenant AND photo_key.source_digest=decode(f.sha256,'hex')
 JOIN photo.billing_usage_charge_items ci ON ci.tenant_id=p_tenant AND ci.usage_request_id=request_id AND ci.photo_identity_id=photo_key.id WHERE f.run_id<>p_run;
 INSERT INTO photo.billing_render_frames(tenant_id,charge_item_id,run_id,task_id,source_version_id,ordinal)
 SELECT p_tenant,ci.id,f.run_id,f.task_id,f.asset_version_id,f.ordinal
 FROM jsonb_to_recordset(frames) AS f(run_id uuid,task_id uuid,asset_version_id uuid,ordinal bigint,sha256 text)
 JOIN photo.billing_photo_identities photo_key ON photo_key.tenant_id=p_tenant AND photo_key.source_digest=decode(f.sha256,'hex')
 JOIN photo.billing_usage_charge_items ci ON ci.tenant_id=p_tenant AND ci.usage_request_id=request_id AND ci.photo_identity_id=photo_key.id;
 -- Batch known deficit is rejected as a whole. Only a single-unit request may use the finite optimistic budget.
 SELECT coalesce(sum(remaining_quantity),0) INTO available FROM photo.billing_credit_buckets WHERE tenant_id=p_tenant AND resource_code='standard_photo' AND state='active' AND starts_at<=n AND (expires_at IS NULL OR expires_at>n);
 SELECT available+coalesce(sum(a.quantity-a.issued_quantity-a.returned_quantity),0) INTO available FROM photo.billing_lease_allocations a JOIN photo.billing_admission_leases l ON l.tenant_id=a.tenant_id AND l.id=a.lease_id
  WHERE a.tenant_id=p_tenant AND a.resource_code='standard_photo' AND a.source='paid' AND l.instance_id=p_instance AND l.period_id=period AND l.state='active' AND l.expires_at>n;
 IF paid>1 AND available<paid THEN RAISE EXCEPTION 'BILLING_QUOTA_EXCEEDED'; END IF;
 FOR rec IN SELECT * FROM photo.billing_usage_charge_items WHERE tenant_id=p_tenant AND usage_request_id=request_id AND max_quantity=1 ORDER BY ordinal LOOP
  SELECT id,fence_token INTO lease_id,fence FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code='standard_photo' AND period_id=period AND instance_id=p_instance AND state='active' AND expires_at>clock_timestamp() AND allocated_quantity+allowance_quantity-issued_quantity-returned_quantity>=1 ORDER BY generation DESC LIMIT 1;
  IF NOT FOUND THEN
   FOR lease_id,fence IN SELECT id,fence_token FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code='standard_photo' AND instance_id=p_instance AND state='active' ORDER BY id LOOP
    PERFORM photo.billing_close_admission_lease(p_tenant,'standard_photo',lease_id,fence);
   END LOOP;
   SELECT coalesce(max(generation),0)+1 INTO gen FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code='standard_photo' AND period_id=period AND instance_id=p_instance;
   lease_id:=photo.billing_allocate_admission_lease(p_tenant,'standard_photo',period,p_instance,gen,1,'apply/'||request_id||'/'||rec.id);
   SELECT fence_token INTO fence FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND id=lease_id;
  END IF;
  claim:=photo.billing_issue_funding_claim(p_tenant,'standard_photo',lease_id,fence,'apply/'||rec.id,p_hash,1);
  INSERT INTO photo.billing_usage_tickets(tenant_id,request_id,charge_item_id,funding_claim_id,quantity) VALUES(p_tenant,request_id,rec.id,claim,1);
 END LOOP;
 RETURN request_id;
END $$;


CREATE TRIGGER billing_render_frames_immutable BEFORE UPDATE OR DELETE ON photo.billing_render_frames FOR EACH ROW EXECUTE FUNCTION photo.billing_quota_reject_immutable();
CREATE TRIGGER billing_render_results_immutable BEFORE UPDATE OR DELETE ON photo.billing_render_results FOR EACH ROW EXECUTE FUNCTION photo.billing_quota_reject_immutable();

CREATE FUNCTION photo.billing_settle_render(p_tenant uuid,p_root uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE req photo.billing_usage_requests%ROWTYPE;rec record;outcome record;success boolean;n timestamptz;failed boolean:=false;proof bytea;
BEGIN
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM photo.billing_quota_lock(p_tenant,'standard_photo');
 SELECT * INTO req FROM photo.billing_usage_requests WHERE tenant_id=p_tenant AND run_id=p_root AND route='V2.Render' FOR UPDATE;
 IF NOT FOUND OR req.status<>'accepted' THEN RETURN false;END IF;
 PERFORM r.id FROM photo.runs r JOIN photo.billing_usage_run_links l ON l.tenant_id=r.tenant_id AND l.run_id=r.id
 JOIN photo.billing_usage_charge_items ci ON ci.tenant_id=l.tenant_id AND ci.id=l.charge_item_id
 WHERE ci.tenant_id=p_tenant AND ci.usage_request_id=req.id ORDER BY r.id FOR SHARE OF r;
 IF EXISTS(SELECT 1 FROM photo.runs r JOIN photo.billing_usage_run_links l ON l.tenant_id=r.tenant_id AND l.run_id=r.id
 JOIN photo.billing_usage_charge_items ci ON ci.tenant_id=l.tenant_id AND ci.id=l.charge_item_id WHERE ci.tenant_id=p_tenant AND ci.usage_request_id=req.id
 AND r.status NOT IN('completed','completed_with_errors','failed','cancelled')) THEN RETURN false;END IF;
 PERFORM t.id FROM photo.tasks t JOIN photo.billing_render_frames f ON f.tenant_id=t.tenant_id AND f.task_id=t.id
 JOIN photo.billing_usage_charge_items ci ON ci.tenant_id=f.tenant_id AND ci.id=f.charge_item_id
 WHERE ci.tenant_id=p_tenant AND ci.usage_request_id=req.id ORDER BY t.id FOR SHARE OF t;
 -- A terminal run with uncertain/missing adoption proof must hold funding, not refund it.
 IF EXISTS(SELECT 1 FROM photo.billing_render_frames f JOIN photo.billing_usage_charge_items ci ON ci.tenant_id=f.tenant_id AND ci.id=f.charge_item_id
 JOIN photo.tasks t ON t.tenant_id=f.tenant_id AND t.id=f.task_id JOIN photo.runs r ON r.tenant_id=f.tenant_id AND r.id=f.run_id
 WHERE ci.tenant_id=p_tenant AND ci.usage_request_id=req.id AND (t.state NOT IN('succeeded','failed','cancelled','skipped') OR
 (t.state='succeeded' AND r.status IN('completed','completed_with_errors','failed') AND (NOT photo.v26_render_task_result_ready(p_tenant,t.id)
 OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 OR EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.review_required AND (r.status<>'failed' OR EXISTS(SELECT 1 FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.task_id=t.id AND v.state='decided' AND v.decision='accept'))
 AND NOT EXISTS(SELECT 1 FROM photo.v26_render_review_selection(p_tenant,t.id))))))) THEN RETURN false;END IF;
 n:=clock_timestamp();
 FOR rec IN SELECT ci.*,ti.id AS ticket_id,ti.funding_claim_id,ti.free_slot_id FROM photo.billing_usage_charge_items ci JOIN photo.billing_usage_tickets ti ON ti.tenant_id=ci.tenant_id AND ti.charge_item_id=ci.id
 WHERE ci.tenant_id=p_tenant AND ci.usage_request_id=req.id AND ci.state='accepted' ORDER BY ci.ordinal LOOP
  outcome:=NULL;
  SELECT t.id AS task_id,z.id AS result_id,root.id AS root_id,img.id AS image_id,
   least(n,greatest(adopted.processed_at,coalesce((SELECT decided_at FROM photo.reviews WHERE tenant_id=p_tenant AND task_id=t.id AND state='decided' LIMIT 1),adopted.processed_at))) AS available_at INTO outcome
  FROM photo.billing_render_frames f JOIN photo.runs r ON r.tenant_id=f.tenant_id AND r.id=f.run_id AND r.status IN('completed','completed_with_errors','failed') AND NOT r.cancel_requested
  JOIN photo.tasks t ON t.tenant_id=f.tenant_id AND t.id=f.task_id AND t.state='succeeded' AND NOT t.cancel_requested
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  JOIN photo.inbox adopted ON adopted.tenant_id=t.tenant_id AND adopted.consumer='v2-adopt' AND adopted.event_id=photo.v2_adoption_event_id(t.id,z.id)
  JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  JOIN photo.v2_artifact_links rl ON rl.tenant_id=w.tenant_id AND rl.workflow_id=w.id AND rl.project_id=w.project_id AND rl.ordinal=0
   AND rl.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
  JOIN photo.artifacts root ON root.tenant_id=rl.tenant_id AND root.id=rl.artifact_id AND root.sha256=rl.sha256
  JOIN photo.v2_artifact_links il ON il.tenant_id=w.tenant_id AND il.workflow_id=w.id AND il.project_id=w.project_id AND il.ordinal=0
   AND il.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
  JOIN photo.artifacts img ON img.tenant_id=il.tenant_id AND img.id=il.artifact_id AND img.sha256=il.sha256 AND img.state='ready' AND img.verified_at IS NOT NULL
   AND img.object_version IS NOT NULL AND img.object_version NOT IN('','null') AND (img.expires_at IS NULL OR img.expires_at>n)
   AND img.created_by_task_id=t.id AND img.produced_attempt=z.attempt AND img.produced_fence=z.fencing_token
  WHERE f.tenant_id=p_tenant AND f.charge_item_id=rec.id AND photo.v26_render_task_result_ready(p_tenant,t.id)
   AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
   AND (NOT z.review_required OR EXISTS(SELECT 1 FROM photo.v26_render_review_selection(p_tenant,t.id) sel WHERE sel.route='candidate' AND sel.image_id=img.id))
  ORDER BY f.ordinal LIMIT 1;
  success:=outcome.task_id IS NOT NULL;
  -- Non-review success requires the adopted visible WorkingImage; absent links are uncertainty.
  IF NOT success AND EXISTS(SELECT 1 FROM photo.billing_render_frames f JOIN photo.tasks t ON t.tenant_id=f.tenant_id AND t.id=f.task_id
   JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id JOIN photo.runs r ON r.tenant_id=f.tenant_id AND r.id=f.run_id
   WHERE f.tenant_id=p_tenant AND f.charge_item_id=rec.id AND t.state='succeeded' AND r.status IN('completed','completed_with_errors','failed') AND NOT r.cancel_requested
    AND (NOT z.review_required OR EXISTS(SELECT 1 FROM photo.v26_render_review_selection(p_tenant,t.id) sel WHERE sel.route='candidate'))) THEN
   RAISE EXCEPTION 'BILLING_RENDER_PROOF_UNAVAILABLE';
  END IF;
  IF success THEN
   INSERT INTO photo.billing_render_results(tenant_id,charge_item_id,task_id,result_id,root_artifact_id,image_artifact_id,adopted_at) VALUES(p_tenant,rec.id,outcome.task_id,outcome.result_id,outcome.root_id,outcome.image_id,outcome.available_at);
   IF rec.charge_kind='initial' THEN
    UPDATE photo.billing_photo_identities SET first_success_at=outcome.available_at,window_ends_at=outcome.available_at+interval '168 hours' WHERE tenant_id=p_tenant AND id=rec.photo_identity_id AND first_success_at IS NULL;
    INSERT INTO photo.billing_photo_free_slots(tenant_id,photo_identity_id,slot_kind,state,window_ends_at) VALUES(p_tenant,rec.photo_identity_id,'rerun_1','available',outcome.available_at+interval '168 hours'),(p_tenant,rec.photo_identity_id,'rerun_2','available',outcome.available_at+interval '168 hours');
   ELSE UPDATE photo.billing_photo_identities SET successful_user_reruns=successful_user_reruns+1 WHERE tenant_id=p_tenant AND id=rec.photo_identity_id;END IF;
  ELSE failed:=true;END IF;
  IF rec.charge_kind='initial' THEN
   UPDATE photo.billing_photo_free_slots SET state=CASE WHEN success THEN 'consumed' ELSE 'available' END,held_at=NULL,usage_charge_item_id=CASE WHEN success THEN rec.id ELSE NULL END WHERE tenant_id=p_tenant AND photo_identity_id=rec.photo_identity_id AND slot_kind='first_result' AND state='held' AND usage_charge_item_id=rec.id;
   IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_INITIAL_SLOT_INCONSISTENT';END IF;
  END IF;
  IF rec.funding_claim_id IS NOT NULL THEN
   proof:=sha256(convert_to('render-terminal-v1/'||p_root||'/'||rec.id||'/'||CASE WHEN success THEN outcome.result_id::text ELSE 'no-result' END,'UTF8'));
   PERFORM photo.billing_finalize_funding_claim(p_tenant,'standard_photo',rec.funding_claim_id,CASE WHEN success THEN 1 ELSE 0 END,proof);
  ELSE
   UPDATE photo.billing_photo_free_slots SET state=CASE WHEN success THEN 'consumed' WHEN window_ends_at>n THEN 'available' ELSE 'expired' END,held_at=NULL,usage_charge_item_id=CASE WHEN success THEN rec.id ELSE NULL END WHERE tenant_id=p_tenant AND id=rec.free_slot_id AND state='held' AND usage_charge_item_id=rec.id;
   IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_FREE_SLOT_INCONSISTENT';END IF;
  END IF;
  UPDATE photo.billing_usage_tickets SET state=CASE WHEN success THEN 'settled' ELSE 'released' END WHERE tenant_id=p_tenant AND id=rec.ticket_id;
  UPDATE photo.billing_usage_charge_items SET state=CASE WHEN success THEN 'succeeded' ELSE 'failed' END WHERE tenant_id=p_tenant AND id=rec.id;
 END LOOP;
 UPDATE photo.billing_usage_requests SET status=CASE WHEN failed THEN 'canceled' ELSE 'settled' END WHERE tenant_id=p_tenant AND id=req.id;
 RETURN true;
END $$;

CREATE FUNCTION photo.billing_settle_next_render() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;done boolean;
BEGIN
 -- Bounded scan, skip busy tenants and nonterminal batches. No ephemeral success authority.
 FOR candidate IN SELECT req.tenant_id,req.run_id FROM photo.billing_usage_requests req
 JOIN photo.runs r ON r.tenant_id=req.tenant_id AND r.id=req.run_id
 JOIN photo.tenants tenant ON tenant.id=req.tenant_id AND tenant.status='active'
 JOIN photo.billing_accounting_controls ctl ON ctl.tenant_id=req.tenant_id AND ctl.lifecycle='active'
 WHERE req.route='V2.Render' AND req.status='accepted' AND (req.render_reconcile_after IS NULL OR req.render_reconcile_after<=clock_timestamp()) AND r.status IN('completed','completed_with_errors','failed','cancelled')
 AND NOT EXISTS(SELECT 1 FROM photo.billing_usage_run_links l JOIN photo.billing_usage_charge_items ci ON ci.tenant_id=l.tenant_id AND ci.id=l.charge_item_id
 JOIN photo.runs c ON c.tenant_id=l.tenant_id AND c.id=l.run_id WHERE ci.usage_request_id=req.id AND ci.tenant_id=req.tenant_id AND c.status NOT IN('completed','completed_with_errors','failed','cancelled'))
 ORDER BY req.created_at,req.id LIMIT 16 LOOP
  PERFORM 1 FROM photo.tenants WHERE id=candidate.tenant_id FOR UPDATE SKIP LOCKED;
  IF FOUND THEN
   done:=false;
   BEGIN
    done:=photo.billing_settle_render(candidate.tenant_id,candidate.run_id);
   EXCEPTION WHEN raise_exception THEN
    IF SQLERRM<>'BILLING_RENDER_PROOF_UNAVAILABLE' THEN RAISE;END IF;
   END;
   IF done THEN RETURN true;END IF;
   UPDATE photo.billing_usage_requests SET render_reconcile_after=clock_timestamp()+interval '30 seconds' WHERE tenant_id=candidate.tenant_id AND run_id=candidate.run_id AND status='accepted';
  END IF;
 END LOOP;
 RETURN false;
END $$;

ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_before_render_billing;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_before_render_billing(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.billing_render_controls WHERE tenant_id=p_tenant AND enabled)
 AND EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='render.compose')
 AND EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.operation='render.compose' AND NOT EXISTS(SELECT 1 FROM photo.billing_render_frames f WHERE f.tenant_id=t.tenant_id AND f.task_id=t.id))
 -- This technical child belongs to its existing Editor Apply authority, never to a fresh render charge.
 AND NOT EXISTS(SELECT 1 FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND final_run_id=p_run) THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_before_render_billing(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;


ALTER FUNCTION photo.billing_prepare_render(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_prepare_render(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.billing_prepare_render(uuid,uuid,uuid,uuid) TO photo_scheduler;

ALTER FUNCTION photo.billing_render_source_frames(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_render_source_frames(uuid,uuid) FROM PUBLIC;

ALTER FUNCTION photo.billing_bind_render(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_bind_render(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.billing_bind_render(uuid,uuid,uuid,uuid,text) TO photo_scheduler;

ALTER FUNCTION photo.billing_settle_render(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_settle_render(uuid,uuid) FROM PUBLIC;

ALTER FUNCTION photo.billing_settle_next_render() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_settle_next_render() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.billing_settle_next_render() TO photo_scheduler;

CREATE OR REPLACE FUNCTION photo.billing_settle_apply_run(p_tenant uuid,p_run uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE req photo.billing_usage_requests%ROWTYPE;r photo.runs%ROWTYPE;rec record;outcome record;success boolean;proof bytea;n timestamptz;has_failed boolean:=false;
BEGIN
 SELECT * INTO req FROM photo.billing_usage_requests WHERE tenant_id=p_tenant AND run_id=p_run AND route<>'V2.Render';
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM photo.billing_lock_apply_run(p_tenant,p_run);
 SELECT * INTO req FROM photo.billing_usage_requests WHERE tenant_id=p_tenant AND run_id=p_run AND route<>'V2.Render' FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF req.status<>'accepted' THEN RETURN; END IF;
 IF r.status NOT IN ('completed','failed','cancelled','completed_with_errors') THEN RETURN; END IF;
 n:=clock_timestamp();
 FOR rec IN SELECT ci.*,ti.id AS ticket_id,ti.funding_claim_id,ti.free_slot_id FROM photo.billing_usage_charge_items ci JOIN photo.billing_usage_tickets ti ON ti.tenant_id=ci.tenant_id AND ti.charge_item_id=ci.id WHERE ci.tenant_id=p_tenant AND ci.usage_request_id=req.id AND ci.state='accepted' ORDER BY ci.ordinal LOOP
  outcome:=NULL;
  SELECT av.id AS version_id,a.id AS artifact_id,tr.id AS result_id INTO outcome FROM photo.collection_items ci JOIN photo.asset_versions av ON av.tenant_id=ci.tenant_id AND av.id=ci.asset_version_id AND av.created_by_run_id=p_run
   JOIN photo.artifacts a ON a.tenant_id=av.tenant_id AND a.id=av.master_artifact_id AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>n)
   JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id AND t.run_id=p_run AND t.state='succeeded'
   JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id AND tr.task_id=t.id AND tr.attempt=a.produced_attempt AND tr.fencing_token=a.produced_fence
   WHERE ci.tenant_id=p_tenant AND ci.collection_id=r.output_collection_id AND ci.created_by_run_id=p_run AND ci.source_item_id=rec.source_item_id
    AND av.parent_asset_version_id=rec.source_version_id AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
  success:=outcome.version_id IS NOT NULL;
  IF r.status IN ('failed','cancelled') AND success THEN RAISE EXCEPTION 'BILLING_TERMINAL_AUTHORITY_CONFLICT'; END IF;
  IF success THEN
   INSERT INTO photo.billing_result_references(tenant_id,charge_item_id,canonical_run_id,final_artifact_id,adopted_result_id,asset_version_id) VALUES(p_tenant,rec.id,p_run,outcome.artifact_id,outcome.result_id,outcome.version_id);
   IF rec.charge_kind='initial' THEN
    UPDATE photo.billing_photo_identities SET first_success_at=n,window_ends_at=n+interval '168 hours' WHERE tenant_id=p_tenant AND id=rec.photo_identity_id AND first_success_at IS NULL;
    INSERT INTO photo.billing_photo_free_slots(tenant_id,photo_identity_id,slot_kind,state,window_ends_at) VALUES(p_tenant,rec.photo_identity_id,'rerun_1','available',n+interval '168 hours'),(p_tenant,rec.photo_identity_id,'rerun_2','available',n+interval '168 hours');
   ELSE UPDATE photo.billing_photo_identities SET successful_user_reruns=successful_user_reruns+1 WHERE tenant_id=p_tenant AND id=rec.photo_identity_id;END IF;
  ELSE has_failed:=true;END IF;
  IF rec.charge_kind='initial' THEN
   UPDATE photo.billing_photo_free_slots SET state=CASE WHEN success THEN 'consumed' ELSE 'available' END,held_at=NULL,usage_charge_item_id=CASE WHEN success THEN rec.id ELSE NULL END WHERE tenant_id=p_tenant AND photo_identity_id=rec.photo_identity_id AND slot_kind='first_result' AND state='held' AND usage_charge_item_id=rec.id;
   IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_INITIAL_SLOT_INCONSISTENT';END IF;
  END IF;
  IF rec.funding_claim_id IS NOT NULL THEN
   proof:=sha256(convert_to('apply-terminal-v1/'||p_run||'/'||rec.id||'/'||CASE WHEN success THEN outcome.version_id::text ELSE r.status END,'UTF8'));
   PERFORM photo.billing_finalize_funding_claim(p_tenant,'standard_photo',rec.funding_claim_id,CASE WHEN success THEN 1 ELSE 0 END,proof);
  ELSE
   UPDATE photo.billing_photo_free_slots SET state=CASE WHEN success THEN 'consumed' WHEN window_ends_at>n THEN 'available' ELSE 'expired' END,
    held_at=NULL,usage_charge_item_id=CASE WHEN success THEN rec.id ELSE NULL END WHERE tenant_id=p_tenant AND id=rec.free_slot_id AND state='held' AND usage_charge_item_id=rec.id;
   IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_FREE_SLOT_INCONSISTENT'; END IF;
  END IF;
  UPDATE photo.billing_usage_tickets SET state=CASE WHEN success THEN 'settled' ELSE 'released' END WHERE tenant_id=p_tenant AND id=rec.ticket_id;
  UPDATE photo.billing_usage_charge_items SET state=CASE WHEN success THEN 'succeeded' ELSE 'failed' END WHERE tenant_id=p_tenant AND id=rec.id;
 END LOOP;
 UPDATE photo.billing_usage_requests SET status=CASE WHEN has_failed THEN 'canceled' ELSE 'settled' END WHERE tenant_id=p_tenant AND id=req.id;
END $$;


CREATE FUNCTION photo.billing_guard_render_terminal() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.status IN('completed','completed_with_errors','failed','cancelled') AND NEW.status IN('draft','queued','running','waiting_review') AND EXISTS(
 SELECT 1 FROM photo.billing_render_frames f JOIN photo.billing_usage_charge_items ci ON ci.tenant_id=f.tenant_id AND ci.id=f.charge_item_id
 JOIN photo.billing_usage_requests req ON req.tenant_id=ci.tenant_id AND req.id=ci.usage_request_id
 WHERE f.tenant_id=OLD.tenant_id AND f.run_id=OLD.id AND req.status<>'accepted') THEN RAISE EXCEPTION 'BILLING_RUN_TERMINAL' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_render_terminal BEFORE UPDATE OF status ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.billing_guard_render_terminal();
ALTER FUNCTION photo.billing_guard_render_terminal() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_guard_render_terminal() FROM PUBLIC;

CREATE FUNCTION photo.billing_guard_source_item() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.source_item_id IS NULL AND NOT EXISTS(SELECT 1 FROM photo.billing_usage_requests WHERE tenant_id=NEW.tenant_id AND id=NEW.usage_request_id AND route='V2.Render') THEN RAISE EXCEPTION 'BILLING_SOURCE_INVALID';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.billing_guard_source_item() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_guard_source_item() FROM PUBLIC;
CREATE TRIGGER billing_source_item_scope BEFORE INSERT OR UPDATE ON photo.billing_usage_charge_items FOR EACH ROW EXECUTE FUNCTION photo.billing_guard_source_item();
