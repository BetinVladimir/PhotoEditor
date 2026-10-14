-- +goose Up
-- Keep a child pending while its next tasks are materialized. Dispatch of the
-- initial derivative already happened under the initial sealed graph.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v30_delivery_continuation(p_expansion uuid,p_token uuid,p_slot integer,p_operation text,
 p_producer jsonb,p_image text,p_report text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;cfg jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 pending photo.graph_expansions%ROWTYPE;initial text;remaining integer;task_id uuid;
BEGIN
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL THEN RETURN false;END IF;
 cfg:=(expected->>'config_jcs')::jsonb;
 IF cfg->'size' IS NOT NULL THEN initial:='derivative.resize';remaining:=CASE WHEN cfg->'sharpen' IS NOT NULL THEN 2 ELSE 1 END;
 ELSIF cfg->'sharpen' IS NOT NULL THEN initial:='derivative.sharpen';remaining:=1;
 ELSE RETURN false;END IF;
 IF p_operation IS DISTINCT FROM initial THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR UPDATE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested OR r.kind<>'v2'
 OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM expected THEN RETURN false;END IF;
 task_id:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),initial||':0');
 IF p_producer->>'task_id' IS DISTINCT FROM task_id::text THEN RETURN false;END IF;
 SELECT * INTO pending FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id
 AND parent_task_id=task_id AND kind='v30.delivery.export/1' FOR UPDATE;
 IF FOUND THEN
  RETURN pending.expected_count=remaining AND pending.state='open'
  AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v30-stage-verified'
   AND event_id=photo.v2_adoption_event_id(task_id,(p_producer->>'result_id')::uuid))
  AND photo.v30_delivery_stage_result(p_expansion,p_token,p_slot,p_operation) IS NOT DISTINCT FROM p_producer;
 END IF;
 IF NOT r.graph_sealed THEN RETURN false;END IF;
 IF NOT photo.record_v30_delivery_stage_verification(p_expansion,p_token,p_slot,p_operation,p_producer,p_image,p_report) THEN RETURN false;END IF;
 INSERT INTO photo.graph_expansions(id,tenant_id,run_id,parent_task_id,kind,state,cursor,expected_count)
 VALUES(photo.v30_uuid5(r.id,'delivery:continuation'),e.tenant_id,r.id,task_id,'v30.delivery.export/1','open','0',remaining);
 UPDATE photo.runs SET graph_sealed=false,revision=revision+1 WHERE tenant_id=e.tenant_id AND id=r.id;
 IF e.expansion_lease_until<=clock_timestamp() THEN
  RAISE EXCEPTION 'DELIVERY_CONTINUATION_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.begin_v30_delivery_continuation(uuid,uuid,integer,text,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v30_delivery_continuation(uuid,uuid,integer,text,jsonb,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v30_delivery_continuation(uuid,uuid,integer,text,jsonb,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
