-- +goose Up
-- Durable failure acknowledgement; no source readiness or lease renewal implied.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_failure_committed(p_expansion uuid,p_token uuid,p_slot integer) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;child uuid;export_id uuid;
BEGIN
 IF p_token IS NULL OR p_slot IS NULL OR p_slot NOT BETWEEN 0 AND 79999 THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion FOR SHARE;
 IF NOT FOUND OR e.kind<>'v2.delivery/1' OR e.state<>'closed' OR e.expansion_token IS DISTINCT FROM p_token
 OR p_slot>=e.cursor::integer THEN RETURN false;END IF;
 child:=photo.v30_uuid5(e.run_id,'child:export:'||p_slot::text);export_id:=photo.v30_uuid5(child,'v30:delivery-export:1');
 RETURN EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v30-delivery-failed'
 AND event_id=photo.v30_uuid5(e.run_id,'delivery:failed:'||p_slot::text))
 AND EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id AND kind='v2' AND status='failed' AND graph_sealed)
 AND EXISTS(SELECT 1 FROM photo.delivery_packages d JOIN photo.v2_workflows w ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id
 JOIN photo.delivery_items i ON i.tenant_id=d.tenant_id AND i.package_id=d.id
 WHERE w.tenant_id=e.tenant_id AND w.run_id=e.run_id AND w.workflow_type='delivery' AND d.state='failed'
 AND d.published_at IS NULL AND d.manifest_artifact_id IS NULL AND i.export_id=export_id AND i.state='failed');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_failure_committed(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_failure_committed(uuid,uuid,integer) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_failure_committed(uuid,uuid,integer) TO photo_scheduler;
CREATE OR REPLACE FUNCTION photo.fail_v30_delivery(p_expansion uuid,p_token uuid,p_slot integer) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;witness jsonb;next_cursor bigint;event_id uuid;
BEGIN
 IF photo.v30_delivery_failure_committed(p_expansion,p_token,p_slot) THEN RETURN true;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=e.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested OR r.graph_sealed THEN RETURN false;END IF;
 witness:=photo.v30_delivery_failure_witness(p_expansion,p_token,p_slot);
 IF witness IS NULL THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=e.run_id FOR UPDATE;
 -- No worker may still own parent work at this pre-assembly barrier.
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND state IN('leased','running'))
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND id<>e.id AND state<>'closed') THEN RETURN false;END IF;
 IF r.revision>=9007199254740991 OR EXISTS(SELECT 1 FROM photo.delivery_packages WHERE tenant_id=e.tenant_id
 AND id=(witness->>'package_id')::uuid AND revision>=9007199254740991) THEN
 RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.delivery_packages SET state='failed',revision=revision+1,updated_at=clock_timestamp()
 WHERE tenant_id=e.tenant_id AND id=(witness->>'package_id')::uuid AND state='building' AND published_at IS NULL AND manifest_artifact_id IS NULL;
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE photo.delivery_items SET state='failed',reason=witness->>'reason',revision=revision+1,updated_at=clock_timestamp()
 WHERE tenant_id=e.tenant_id AND package_id=(witness->>'package_id')::uuid AND export_id=(witness->>'export_id')::uuid;
 IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_FAILURE_ITEM_MISSING' USING ERRCODE='23514';END IF;
 UPDATE photo.tasks SET state='cancelled',cancel_requested=true WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND state IN('blocked','ready');
 UPDATE photo.graph_expansions SET state='closed' WHERE id=e.id;
 UPDATE photo.runs SET status='failed',graph_sealed=true,revision=revision+1 WHERE tenant_id=e.tenant_id AND id=e.run_id;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(e.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=e.tenant_id AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=photo.v30_uuid5(e.run_id,'delivery:failed:1');
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(e.tenant_id,next_cursor,e.run_id,r.created_by,'principal','v2.run.failed',jsonb_build_object('run_id',e.run_id,'status','failed','reason',witness->>'reason'));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(e.tenant_id,event_id,'photo.events.v1','v2.run.failed',jsonb_build_object('schema_version',1,'event_id',event_id,
 'type','v2.run.failed','tenant_id',e.tenant_id,'run_id',e.run_id,'status','failed','reason',witness->>'reason'));
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_FAILURE_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(e.tenant_id,'v30-delivery-failed',
 photo.v30_uuid5(e.run_id,'delivery:failed:'||p_slot::text),clock_timestamp()) ON CONFLICT DO NOTHING;
 RETURN true;
END $$;
ALTER FUNCTION photo.fail_v30_delivery(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_v30_delivery(uuid,uuid,integer) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.fail_v30_delivery(uuid,uuid,integer) TO photo_scheduler;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
