-- +goose Up
-- Dedicated semantic/physical adoption and package transition, one transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v30_delivery_assembled(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text,p_params text,p_body text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;d photo.delivery_packages%ROWTYPE;
 root jsonb;shard_raw jsonb;slot jsonb;require_review boolean;next_state text;next_cursor bigint;event_id uuid;updated_count integer:=0;
BEGIN
 authority:=photo.v30_delivery_manifest_authority(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_body);
 IF authority IS NULL THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=p_tenant AND id=(authority->>'package_id')::uuid FOR UPDATE;
 IF r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed OR d.state<>'building' OR d.manifest_artifact_id IS NOT NULL
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=r.id AND state<>'closed')
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND state<>'succeeded') THEN RETURN false;END IF;
 IF r.revision>=9007199254740991 OR d.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 IF photo.adopt_v2_plan_pre_v30_assembly(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 root:=(authority->>'assembly_jcs')::jsonb;require_review:=(root->'policy'->>'require_review')::boolean;
 IF require_review IS NULL THEN RAISE EXCEPTION 'DELIVERY_REVIEW_POLICY_INVALID' USING ERRCODE='23514';END IF;
 FOR shard_raw IN SELECT value FROM jsonb_array_elements(authority->'shards_jcs') LOOP
  FOR slot IN SELECT value FROM jsonb_array_elements((shard_raw#>>'{}')::jsonb->'items') LOOP
   UPDATE photo.delivery_items SET state=slot->>'state',reason=slot->>'reason',export_id=(slot->>'export_id')::uuid,revision=revision+1,updated_at=clock_timestamp()
   WHERE tenant_id=p_tenant AND package_id=d.id AND ordinal=(slot->>'ordinal')::integer AND preset_id=(slot->>'preset_id')::uuid
   AND preset_sha=slot->>'preset_sha' AND required=(slot->>'required')::boolean AND revision<9007199254740991;
   IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_ITEM_ADOPTION_MISMATCH' USING ERRCODE='23514';END IF;
   updated_count:=updated_count+1;
  END LOOP;
 END LOOP;
 IF updated_count<>(SELECT count(*) FROM photo.delivery_items WHERE tenant_id=p_tenant AND package_id=d.id) THEN
 RAISE EXCEPTION 'DELIVERY_ITEM_COUNT_MISMATCH' USING ERRCODE='23514';END IF;
 UPDATE photo.delivery_packages SET manifest_artifact_id=(authority->>'manifest_artifact_id')::uuid,
 state=CASE WHEN require_review THEN 'review' ELSE 'ready' END,revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=d.id;
 next_state:=CASE WHEN require_review THEN 'waiting_review' ELSE 'completed' END;
 IF require_review THEN
  INSERT INTO photo.reviews(id,tenant_id,run_id,task_id,candidate_artifact_id,candidate_hash,kind,reasons)
  VALUES(photo.v30_uuid5(r.id,'delivery:review:1'),p_tenant,r.id,t.id,(authority->>'manifest_artifact_id')::uuid,authority->>'manifest_sha','delivery','[]'::jsonb);
 END IF;
 UPDATE photo.runs SET status=next_state,revision=revision+1 WHERE tenant_id=p_tenant AND id=r.id;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=photo.v30_uuid5(r.id,'delivery:assembled:1');
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,r.id,r.created_by,'principal','delivery.assembled',jsonb_build_object('run_id',r.id,'package_id',d.id,'status',next_state,'manifest_sha',authority->>'manifest_sha'));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','delivery.assembled',jsonb_build_object('schema_version',1,'event_id',event_id,'type','delivery.assembled','tenant_id',p_tenant,'run_id',r.id,'package_id',d.id,'status',next_state));
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v30_delivery_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_delivery_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_delivery_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
