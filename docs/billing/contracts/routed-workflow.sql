-- +goose Up
ALTER TABLE photo.billing_usage_requests DROP CONSTRAINT billing_usage_requests_route_check;
ALTER TABLE photo.billing_usage_requests DROP CONSTRAINT billing_usage_requests_workflow_kind_check;
ALTER TABLE photo.billing_usage_requests ADD CONSTRAINT billing_usage_requests_route_kind_check CHECK (
 (route='Run.Apply' AND workflow_kind='standard_apply') OR
 (route='SessionAnalysis.Apply' AND workflow_kind='session_analysis_apply') OR
 (route='Run.SaveEdits' AND workflow_kind='save_edits_style'));
CREATE OR REPLACE FUNCTION photo.billing_prepare_apply(p_tenant uuid,p_actor uuid,p_session uuid,p_route text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v_role text;expired_lease record;
BEGIN
 IF nullif(current_setting('app.tenant_id',true),'')::uuid IS DISTINCT FROM p_tenant THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID'; END IF;
 SELECT role INTO v_role FROM photo.authorize_tenant(p_actor,p_session,p_tenant);
 IF v_role IS NULL OR v_role NOT IN ('owner','admin','editor') THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID'; END IF;
 IF p_route NOT IN ('Run.Apply','SessionAnalysis.Apply','Run.SaveEdits') OR p_route IS NULL THEN RETURN false; END IF;
 PERFORM 1 FROM photo.billing_workflow_controls WHERE tenant_id=p_tenant AND enabled FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM photo.billing_quota_lock(p_tenant,'standard_photo');
 IF NOT EXISTS(SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) a WHERE a.role IN ('owner','admin','editor')) THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID'; END IF;
 -- Recover a bounded number of expired issuers, including previous process instances.
 -- Close returns only durable unissued funding and retains accepted claims.
 FOR expired_lease IN SELECT id,fence_token FROM photo.billing_admission_leases WHERE tenant_id=p_tenant AND resource_code='standard_photo' AND state='active' AND expires_at<=clock_timestamp() ORDER BY expires_at,id LIMIT 16 LOOP
  PERFORM photo.billing_close_admission_lease(p_tenant,'standard_photo',expired_lease.id,expired_lease.fence_token);
 END LOOP;
 RETURN true;
END $$;

CREATE FUNCTION photo.billing_bind_routed_apply(p_tenant uuid,p_actor uuid,p_session uuid,p_run uuid,p_key text,p_hash bytea,p_instance text,p_route text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; req photo.billing_usage_requests%ROWTYPE; rec record; ident photo.billing_photo_identities%ROWTYPE;
 slot_id uuid; item_id uuid; ticket uuid; quote uuid; request_id uuid; period uuid; paid bigint:=0; available numeric; lease_id uuid; fence uuid;
 gen bigint; claim uuid; decision text; q bigint; n timestamptz; valid_count bigint; expected bigint;
BEGIN
 IF NOT photo.billing_prepare_apply(p_tenant,p_actor,p_session,p_route) THEN RETURN NULL; END IF;
 IF p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_hash IS NULL OR octet_length(p_hash)<>32 OR p_instance IS NULL OR length(p_instance) NOT BETWEEN 1 AND 120 THEN RAISE EXCEPTION 'BILLING_REQUEST_INVALID'; END IF;
 SELECT * INTO req FROM photo.billing_usage_requests WHERE tenant_id=p_tenant AND actor_user_id=p_actor AND route=p_route AND request_key=p_key;
 IF FOUND THEN
  IF req.canonical_request_hash IS DISTINCT FROM p_hash OR req.run_id<>p_run THEN RAISE EXCEPTION 'BILLING_IDEMPOTENCY_CONFLICT'; END IF;
  RETURN req.id;
 END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.created_by<>p_actor OR r.kind<>'apply' OR r.status<>'queued' OR r.graph_sealed OR r.cancel_requested OR r.output_collection_id IS NULL
  OR NOT EXISTS(SELECT 1 FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run AND phase IN ('resolve','mask'))
  OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run) THEN RAISE EXCEPTION 'BILLING_RUN_INVALID'; END IF;
 -- Route comes from the server entry point; independently check the persisted run intent.
 IF (p_route='SessionAnalysis.Apply' AND coalesce(r.config_snapshot->>'analysis_decision_sha','') !~ '^[0-9a-f]{64}$')
 OR (p_route<>'SessionAnalysis.Apply' AND r.config_snapshot ? 'analysis_decision_sha')
 OR (p_route='Run.SaveEdits' AND (coalesce(r.config_snapshot->>'identity_style','true')<>'false' OR coalesce(r.config_snapshot->>'overrides_id','') !~ '^[0-9a-f-]{36}$')) THEN
  RAISE EXCEPTION 'BILLING_ROUTE_INVALID';
 END IF;
 IF EXISTS(SELECT 1 FROM photo.billing_usage_requests WHERE tenant_id=p_tenant AND run_id=p_run) THEN RAISE EXCEPTION 'BILLING_IDEMPOTENCY_CONFLICT'; END IF;
 -- Match the existing project authorization, including restricted project ACL.
 IF NOT EXISTS(SELECT 1 FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id AND c.deleted_at IS NULL
  JOIN photo.projects p ON p.tenant_id=c.tenant_id AND p.id=c.project_id AND p.archived_at IS NULL
  WHERE s.tenant_id=p_tenant AND s.id=r.input_snapshot_id AND s.principal_type='user' AND s.principal_id=p_actor
   AND (EXISTS(SELECT 1 FROM photo.memberships m WHERE m.tenant_id=p_tenant AND m.user_id=p_actor AND m.status='active' AND m.role IN ('owner','admin'))
    OR (p.visibility<>'restricted' OR EXISTS(SELECT 1 FROM photo.project_acl a WHERE a.tenant_id=p_tenant AND a.project_id=p.id AND a.user_id=p_actor AND a.role IN ('owner','admin','editor'))))) THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID'; END IF;
 n:=clock_timestamp();
 SELECT id INTO period FROM photo.billing_periods WHERE tenant_id=p_tenant AND state='open' AND starts_at<=n AND ends_at>n;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_PERIOD_INVALID'; END IF;
 SELECT item_count INTO expected FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id;
 SELECT count(*) INTO valid_count FROM photo.snapshot_items si JOIN photo.collection_items ci ON ci.tenant_id=si.tenant_id AND ci.id=si.item_id AND ci.asset_version_id=si.asset_version_id
  JOIN photo.assets a ON a.tenant_id=ci.tenant_id AND a.id=ci.asset_id AND a.deleted_at IS NULL AND a.upload_state='verified'
  JOIN photo.artifacts orig ON orig.tenant_id=a.tenant_id AND orig.id=a.original_artifact_id AND orig.state='ready' AND orig.object_version IS NOT NULL AND (orig.expires_at IS NULL OR orig.expires_at>n)
  WHERE si.tenant_id=p_tenant AND si.snapshot_id=r.input_snapshot_id;
 IF expected IS NULL OR expected NOT BETWEEN 1 AND 5000 OR valid_count<>expected THEN RAISE EXCEPTION 'BILLING_SOURCE_INVALID'; END IF;
 quote:=gen_random_uuid();request_id:=gen_random_uuid();
 -- The cost ceiling and all slot decisions are pinned before any task can execute.
 INSERT INTO photo.billing_cost_quotes(id,tenant_id,actor_id,request_hash,snapshot_id,policy_version,max_quantity,expires_at,state)
 VALUES(quote,p_tenant,p_actor,p_hash,r.input_snapshot_id,1,expected,n+interval '15 minutes','accepted');
 INSERT INTO photo.billing_usage_requests(id,tenant_id,actor_user_id,session_id,request_key,route,canonical_request_hash,cost_quote_id,run_id,period_id,workflow_kind)
 VALUES(request_id,p_tenant,p_actor,p_session,p_key,p_route,p_hash,quote,p_run,period,CASE p_route WHEN 'SessionAnalysis.Apply' THEN 'session_analysis_apply' WHEN 'Run.SaveEdits' THEN 'save_edits_style' ELSE 'standard_apply' END);
 FOR rec IN SELECT DISTINCT ON (orig.sha256) orig.sha256,si.ordinal,si.item_id,si.asset_version_id FROM photo.snapshot_items si
  JOIN photo.collection_items ci ON ci.tenant_id=si.tenant_id AND ci.id=si.item_id
  JOIN photo.assets a ON a.tenant_id=ci.tenant_id AND a.id=ci.asset_id JOIN photo.artifacts orig ON orig.tenant_id=a.tenant_id AND orig.id=a.original_artifact_id
  WHERE si.tenant_id=p_tenant AND si.snapshot_id=r.input_snapshot_id ORDER BY orig.sha256,si.ordinal LOOP
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
  INSERT INTO photo.billing_usage_charge_items(tenant_id,usage_request_id,ordinal,photo_identity_id,source_item_id,source_version_id,charge_kind,max_quantity)
  VALUES(p_tenant,request_id,rec.ordinal,ident.id,rec.item_id,rec.asset_version_id,decision,q) RETURNING id INTO item_id;
  IF decision='initial' THEN
   INSERT INTO photo.billing_photo_free_slots(tenant_id,photo_identity_id,slot_kind,state,usage_charge_item_id,held_at) VALUES(p_tenant,ident.id,'first_result','held',item_id,n) ON CONFLICT(tenant_id,photo_identity_id,slot_kind) DO UPDATE SET state='held',usage_charge_item_id=EXCLUDED.usage_charge_item_id,held_at=EXCLUDED.held_at;
  END IF;
  IF q=0 THEN
   UPDATE photo.billing_photo_free_slots SET state='held',usage_charge_item_id=item_id,held_at=n WHERE id=slot_id AND tenant_id=p_tenant;
   INSERT INTO photo.billing_usage_tickets(tenant_id,request_id,charge_item_id,quantity,free_slot_id) VALUES(p_tenant,request_id,item_id,0,slot_id);
  ELSE paid:=paid+1;END IF;
  INSERT INTO photo.billing_usage_run_links(tenant_id,charge_item_id,run_id,relation) VALUES(p_tenant,item_id,p_run,'root');
 END LOOP;
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

CREATE OR REPLACE FUNCTION photo.billing_bind_apply(p_tenant uuid,p_actor uuid,p_session uuid,p_run uuid,p_key text,p_hash bytea,p_instance text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RETURN photo.billing_bind_routed_apply(p_tenant,p_actor,p_session,p_run,p_key,p_hash,p_instance,'Run.Apply');
END $$;
ALTER FUNCTION photo.billing_prepare_apply(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_prepare_apply(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.billing_prepare_apply(uuid,uuid,uuid,text) TO photo_api;
ALTER FUNCTION photo.billing_bind_routed_apply(uuid,uuid,uuid,uuid,text,bytea,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_bind_routed_apply(uuid,uuid,uuid,uuid,text,bytea,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.billing_bind_routed_apply(uuid,uuid,uuid,uuid,text,bytea,text,text) TO photo_api;
ALTER FUNCTION photo.billing_bind_apply(uuid,uuid,uuid,uuid,text,bytea,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_bind_apply(uuid,uuid,uuid,uuid,text,bytea,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.billing_bind_apply(uuid,uuid,uuid,uuid,text,bytea,text) TO photo_api;
