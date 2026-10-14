-- +goose Up
-- Existing packages must remain claimable by the same installed signer after
-- its window closes. The API admission gate still requires an active window.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_delivery_expansion_instance_ready(
 p_instance uuid,p_bucket text,p_expansion uuid) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE policy jsonb;tenant uuid;
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR p_expansion IS NULL THEN RETURN false;END IF;
 SELECT e.tenant_id,(b.v30_delivery_proof->>'frozen_input_jcs')::jsonb->'policy'
 INTO STRICT tenant,policy
 FROM photo.graph_expansions e
 JOIN photo.tasks t ON t.tenant_id=e.tenant_id AND t.run_id=e.run_id AND t.id=e.parent_task_id
 JOIN photo.backend_documents b ON b.tenant_id=e.tenant_id AND b.run_id=e.run_id
  AND b.owner_id=t.id AND b.purpose='task_params' AND b.artifact_id=t.params_artifact_id
  AND b.state='ready'
 WHERE e.id=p_expansion AND e.kind='v2.delivery/1';
 IF photo.v31_delivery_frozen_signing_choice_valid(policy) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF policy->>'provenance'='off' THEN
  RETURN EXISTS(SELECT 1 FROM photo.v30_delivery_runtime_leases l
   WHERE l.instance_id=p_instance AND l.bucket=p_bucket AND l.expires_at>clock_timestamp());
 END IF;
 RETURN EXISTS(
  SELECT 1 FROM photo.v30_delivery_runtime_leases l,
   LATERAL jsonb_array_elements(l.signing_policy_pins) pin(value)
  WHERE l.instance_id=p_instance AND l.bucket=p_bucket AND l.expires_at>clock_timestamp()
   AND tenant=ANY(l.signing_tenants)
   AND pin.value->>'tenant_id'=tenant::text
   AND pin.value->>'policy_id'=policy->>'signing_policy_id'
   AND pin.value->>'policy_sha'=policy->>'signing_policy_sha'
   AND (pin.value->>'not_before')::timestamptz<=clock_timestamp());
EXCEPTION WHEN no_data_found OR too_many_rows OR invalid_text_representation
 OR invalid_datetime_format OR datetime_field_overflow THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_expansion_instance_ready(uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_expansion_instance_ready(uuid,text,uuid)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;

-- A retained exact scheduler policy pin proves the finite expiry window.
-- Close only after every unsigned delivery item is ready; never infer failure
-- from a missing scheduler lease, transient key error, or a future window.
-- +goose StatementBegin
CREATE FUNCTION photo.fail_v31_delivery_expired_policy(
 p_expansion uuid,p_token uuid,p_instance uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE x photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.delivery_packages%ROWTYPE;i photo.delivery_items%ROWTYPE;
 policy jsonb;bucket text;event_id uuid;next_cursor bigint;
BEGIN
 IF p_token IS NULL OR p_instance IS NULL THEN RETURN false;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=x.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=x.tenant_id AND id=x.run_id FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested
 OR r.graph_sealed OR r.revision>=9007199254740991 THEN RETURN false;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion AND tenant_id=r.tenant_id
 AND run_id=r.id AND kind='v2.delivery/1' AND state='open'
 AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()
 AND cursor=expected_count FOR UPDATE;
 IF NOT FOUND OR photo.v30_delivery_expansion_ready(x.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT l.bucket INTO bucket FROM photo.v30_delivery_runtime_leases l
 WHERE l.instance_id=p_instance AND l.expires_at>clock_timestamp() FOR SHARE;
 IF bucket IS NULL THEN RETURN false;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb->'policy' INTO STRICT policy
 FROM photo.backend_documents b JOIN photo.tasks t
 ON t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.id=b.owner_id
 WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id AND b.owner_id=x.parent_task_id
 AND b.purpose='task_params' AND b.artifact_id=t.params_artifact_id AND b.state='ready';
 IF photo.v31_delivery_frozen_signing_choice_valid(policy) IS DISTINCT FROM true
 OR policy->>'provenance'<>'required' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v30_delivery_runtime_leases l,
  LATERAL jsonb_array_elements(l.signing_policy_pins) pin(value)
  WHERE l.instance_id=p_instance AND l.bucket=bucket AND l.expires_at>clock_timestamp()
  AND x.tenant_id=ANY(l.signing_tenants)
  AND pin.value->>'tenant_id'=x.tenant_id::text
  AND pin.value->>'policy_id'=policy->>'signing_policy_id'
  AND pin.value->>'policy_sha'=policy->>'signing_policy_sha'
  AND (pin.value->>'not_after')::timestamptz<=clock_timestamp())
 OR photo.v30_delivery_signing_runtime_ready(bucket,x.tenant_id,
  (policy->>'signing_policy_id')::uuid,policy->>'signing_policy_sha') IS DISTINCT FROM false
 THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=x.tenant_id AND run_id=x.run_id
 AND workflow_type='delivery' AND renderer_version='v2.2' FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=x.tenant_id
 AND workflow_id=w.id AND project_id=w.project_id AND state='building'
 AND published_at IS NULL AND manifest_artifact_id IS NULL FOR UPDATE;
 IF d.id IS NULL OR d.revision>=9007199254740991 THEN RETURN false;END IF;
 PERFORM id FROM photo.delivery_items WHERE tenant_id=x.tenant_id AND package_id=d.id
 ORDER BY id FOR UPDATE;
 IF (SELECT count(*)=x.expected_count AND bool_and(state='ready' AND export_id IS NOT NULL)
  FROM photo.delivery_items WHERE tenant_id=x.tenant_id AND package_id=d.id) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 SELECT * INTO i FROM photo.delivery_items WHERE tenant_id=x.tenant_id AND package_id=d.id
 ORDER BY ordinal,preset_id LIMIT 1 FOR UPDATE;
 IF i.id IS NULL OR i.revision>=9007199254740991 THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=x.tenant_id AND run_id=x.run_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=x.tenant_id AND run_id=x.run_id
  AND state IN('leased','running'))
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=x.tenant_id
  AND run_id=x.run_id AND id<>x.id AND state<>'closed') THEN RETURN false;END IF;
 UPDATE photo.delivery_packages SET state='failed',revision=revision+1,
 updated_at=clock_timestamp() WHERE tenant_id=x.tenant_id AND id=d.id AND state='building';
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE photo.delivery_items SET state='failed',reason='SIGNING_POLICY_EXPIRED',
 revision=revision+1,updated_at=clock_timestamp()
 WHERE tenant_id=x.tenant_id AND id=i.id AND state='ready';
 IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_POLICY_EXPIRY_ITEM_MISSING' USING ERRCODE='23514';END IF;
 UPDATE photo.tasks SET state='cancelled',cancel_requested=true
 WHERE tenant_id=x.tenant_id AND run_id=x.run_id AND state IN('blocked','ready');
 UPDATE photo.graph_expansions SET state='closed' WHERE id=x.id;
 UPDATE photo.runs SET status='failed',graph_sealed=true,revision=revision+1
 WHERE tenant_id=x.tenant_id AND id=x.run_id;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor)
 VALUES(x.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
 WHERE tenant_id=x.tenant_id AND last_cursor<9007199254740991
 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=photo.v30_uuid5(x.run_id,'delivery:signing:policy-expired');
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(x.tenant_id,next_cursor,x.run_id,r.created_by,'principal','v2.run.failed',
 jsonb_build_object('run_id',x.run_id,'status','failed','reason','SIGNING_POLICY_EXPIRED'));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(x.tenant_id,event_id,'photo.events.v1','v2.run.failed',
 jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.failed',
 'tenant_id',x.tenant_id,'run_id',x.run_id,'status','failed','reason','SIGNING_POLICY_EXPIRED'));
 IF x.expansion_lease_until<=clock_timestamp() THEN
  RAISE EXCEPTION 'DELIVERY_POLICY_EXPIRY_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
 VALUES(x.tenant_id,'v31-delivery-policy-expired',event_id,clock_timestamp())
 ON CONFLICT DO NOTHING;
 RETURN true;
EXCEPTION WHEN no_data_found OR too_many_rows OR invalid_text_representation
 OR invalid_datetime_format OR datetime_field_overflow OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
