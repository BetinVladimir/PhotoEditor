-- +goose Up
-- A terminal delegated signer cannot leave a ready unsigned Delivery item in
-- a permanently building package. Only the current complete parent lease may
-- close the package, after re-reading the exact deterministic signer status.
-- +goose StatementBegin
CREATE FUNCTION photo.fail_v31_delivery_signing(p_expansion uuid,p_token uuid,
 p_ordinal integer,p_preset uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE x photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.delivery_packages%ROWTYPE;i photo.delivery_items%ROWTYPE;
 observed text;export_id uuid;signer_task uuid;v_reason text;event_id uuid;next_cursor bigint;
BEGIN
 IF p_token IS NULL OR p_preset IS NULL OR p_ordinal IS NULL
 OR p_ordinal NOT BETWEEN 0 AND 4999 THEN RETURN false;END IF;
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
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=x.tenant_id AND run_id=x.run_id
 AND workflow_type='delivery' AND renderer_version='v2.2' FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=x.tenant_id
 AND workflow_id=w.id AND project_id=w.project_id AND state='building'
 AND published_at IS NULL AND manifest_artifact_id IS NULL FOR UPDATE;
 IF d.id IS NULL OR d.revision>=9007199254740991 THEN RETURN false;END IF;
 SELECT * INTO i FROM photo.delivery_items WHERE tenant_id=x.tenant_id
 AND package_id=d.id AND project_id=d.project_id AND ordinal=p_ordinal
 AND preset_id=p_preset AND state='ready' AND export_id IS NOT NULL FOR UPDATE;
 IF i.id IS NULL OR i.revision>=9007199254740991 THEN RETURN false;END IF;
 SELECT signing_status,read_status.export_id,task_id INTO observed,export_id,signer_task
 FROM photo.read_v30_delivery_signer_status(p_expansion,p_token,p_ordinal,p_preset) read_status;
 IF observed IS NULL OR observed NOT IN('failed','cancelled') OR export_id IS DISTINCT FROM i.export_id
 OR signer_task IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=x.tenant_id AND run_id=x.run_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=x.tenant_id AND run_id=x.run_id
  AND state IN('leased','running'))
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=x.tenant_id
  AND run_id=x.run_id AND id<>x.id AND state<>'closed') THEN RETURN false;END IF;
 v_reason:=CASE WHEN observed='cancelled' THEN 'SIGNING_CANCELLED' ELSE 'SIGNING_FAILED' END;
 UPDATE photo.delivery_packages SET state='failed',revision=revision+1,
 updated_at=clock_timestamp() WHERE tenant_id=x.tenant_id AND id=d.id AND state='building';
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE photo.delivery_items SET state='failed',reason=v_reason,revision=revision+1,
 updated_at=clock_timestamp() WHERE tenant_id=x.tenant_id AND id=i.id AND state='ready';
 IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_SIGNING_FAILURE_ITEM_MISSING' USING ERRCODE='23514';END IF;
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
 event_id:=photo.v30_uuid5(x.run_id,'delivery:signing:failed:'||p_ordinal::text||':'||p_preset::text);
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(x.tenant_id,next_cursor,x.run_id,r.created_by,'principal','v2.run.failed',
 jsonb_build_object('run_id',x.run_id,'status','failed','reason',v_reason));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(x.tenant_id,event_id,'photo.events.v1','v2.run.failed',
 jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.failed',
 'tenant_id',x.tenant_id,'run_id',x.run_id,'status','failed','reason',v_reason));
 IF x.expansion_lease_until<=clock_timestamp() THEN
  RAISE EXCEPTION 'DELIVERY_SIGNING_FAILURE_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
 VALUES(x.tenant_id,'v31-delivery-signing-failed',event_id,clock_timestamp())
 ON CONFLICT DO NOTHING;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.fail_v31_delivery_signing(uuid,uuid,integer,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_v31_delivery_signing(uuid,uuid,integer,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.fail_v31_delivery_signing(uuid,uuid,integer,uuid)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
