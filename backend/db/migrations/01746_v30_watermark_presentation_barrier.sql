-- +goose Up
-- Dedicated replayable barrier for exactly one encode task after watermark.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v30_watermark_presentation_continuation(p_expansion uuid,p_token uuid,p_slot integer,p_operation text,
 p_producer jsonb,p_image text,p_report text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;cfg jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 old_barrier photo.graph_expansions%ROWTYPE;pending photo.graph_expansions%ROWTYPE;initial text;remaining integer;task_id uuid;
BEGIN
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL THEN RETURN false;END IF;
 cfg:=(expected->>'config_jcs')::jsonb;
 IF jsonb_typeof(cfg->'watermark') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF cfg->'size' IS NULL AND cfg->'sharpen' IS NULL THEN RETURN false;END IF;
 IF photo.lock_v30_watermark_stage_source(p_expansion,p_token,p_slot) IS DISTINCT FROM true THEN RETURN false;END IF;
 initial:=CASE WHEN cfg->'sharpen' IS NOT NULL THEN 'derivative.sharpen' ELSE 'derivative.resize' END;remaining:=1;
 IF p_operation IS DISTINCT FROM initial THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR UPDATE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested OR r.kind<>'v2'
 OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM expected THEN RETURN false;END IF;
 task_id:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),initial||':0');
 IF p_producer->>'task_id' IS DISTINCT FROM task_id::text THEN RETURN false;END IF;
 SELECT * INTO pending FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id
 AND parent_task_id=task_id AND kind='v30.delivery.presentation_watermark/1' FOR UPDATE;
 IF FOUND THEN
  IF NOT photo.record_v30_delivery_stage_verification(p_expansion,p_token,p_slot,p_operation,p_producer,p_image,p_report) THEN RETURN false;END IF;
  RETURN pending.expected_count=remaining AND pending.state='open'
  AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v30-stage-verified'
   AND event_id=photo.v2_adoption_event_id(task_id,(p_producer->>'result_id')::uuid))
  AND photo.v30_delivery_stage_result(p_expansion,p_token,p_slot,p_operation) IS NOT DISTINCT FROM p_producer;
 END IF;
 SELECT * INTO old_barrier FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id
 AND kind='v30.delivery.export/1' AND state='open' FOR UPDATE;
 IF FOUND THEN
  IF cfg->'size' IS NULL OR cfg->'sharpen' IS NULL OR old_barrier.expected_count<>2 OR old_barrier.cursor IS DISTINCT FROM '1'
  OR old_barrier.parent_task_id<>photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'derivative.resize:0') THEN RETURN false;END IF;
 ELSIF NOT r.graph_sealed THEN RETURN false;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.graph_expansions x WHERE x.tenant_id=e.tenant_id AND x.run_id=r.id AND x.state='open'
 AND (old_barrier.id IS NULL OR x.id<>old_barrier.id)) THEN RETURN false;END IF;
 IF NOT photo.record_v30_delivery_stage_verification(p_expansion,p_token,p_slot,p_operation,p_producer,p_image,p_report) THEN RETURN false;END IF;
 IF old_barrier.id IS NOT NULL THEN UPDATE photo.graph_expansions SET cursor='2',state='closed' WHERE id=old_barrier.id;END IF;
 INSERT INTO photo.graph_expansions(id,tenant_id,run_id,parent_task_id,kind,state,cursor,expected_count)
 VALUES(photo.v30_uuid5(r.id,'delivery:presentation-watermark:continuation'),e.tenant_id,r.id,task_id,'v30.delivery.presentation_watermark/1','open','0',remaining);
 UPDATE photo.runs SET graph_sealed=false,revision=revision+1 WHERE tenant_id=e.tenant_id AND id=r.id;
 IF e.expansion_lease_until<=clock_timestamp() THEN
  RAISE EXCEPTION 'DELIVERY_CONTINUATION_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.begin_v30_watermark_presentation_continuation(uuid,uuid,integer,text,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v30_watermark_presentation_continuation(uuid,uuid,integer,text,jsonb,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.begin_v30_watermark_presentation_continuation(uuid,uuid,integer,text,jsonb,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
