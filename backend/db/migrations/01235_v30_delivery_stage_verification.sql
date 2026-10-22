-- +goose Up
-- Dedicated physical-verification receipt. It intentionally does not write
-- v2-adopt: finalization stays blocked until continuation graph commit exists.
-- +goose StatementBegin
CREATE FUNCTION photo.record_v30_delivery_stage_verification(p_expansion uuid,p_token uuid,p_slot integer,p_operation text,
 p_producer jsonb,p_image text,p_report text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE fresh jsonb;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;item jsonb;image jsonb;report jsonb;
 marker uuid;image_ref jsonb;report_ref jsonb;pixels_ref jsonb;alpha_ref jsonb;
BEGIN
 IF p_producer IS NULL OR p_image IS NULL OR p_report IS NULL
 OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 OR octet_length(p_report) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 fresh:=photo.v30_delivery_stage_result(p_expansion,p_token,p_slot,p_operation);
 IF fresh IS NULL OR fresh IS DISTINCT FROM p_producer THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND id=(fresh->>'task_id')::uuid FOR UPDATE;
 IF NOT FOUND OR t.attempt IS DISTINCT FROM (fresh->>'attempt')::integer
 OR t.fencing_token IS DISTINCT FROM (fresh->>'fence')::bigint
 OR t.canonical_result_id IS DISTINCT FROM (fresh->>'result_id')::uuid OR t.state<>'succeeded' OR t.cancel_requested THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(fresh->'outputs') LOOP
  IF item->>'role'='working_image' THEN
   image_ref:=item->'ref';
   IF encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM image_ref->>'sha256'
   OR octet_length(p_image) IS DISTINCT FROM (item->>'bytes')::bigint THEN RETURN false;END IF;
  ELSIF item->>'role'='derivative_report' THEN
   report_ref:=item->'ref';
   IF encode(sha256(convert_to(p_report,'UTF8')),'hex') IS DISTINCT FROM report_ref->>'sha256'
   OR octet_length(p_report) IS DISTINCT FROM (item->>'bytes')::bigint THEN RETURN false;END IF;
  ELSIF item->>'role'='working_rgb' THEN pixels_ref:=item->'ref';
  ELSIF item->>'role'='working_alpha' THEN alpha_ref:=item->'ref';END IF;
 END LOOP;
 image:=p_image::jsonb;report:=p_report::jsonb;
 IF image_ref IS NULL OR report_ref IS NULL OR pixels_ref IS NULL
 OR image->'pixels' IS DISTINCT FROM pixels_ref
 OR coalesce(image->'alpha','null'::jsonb) IS DISTINCT FROM coalesce(alpha_ref,'null'::jsonb)
 OR report->>'operation' IS DISTINCT FROM (CASE p_operation WHEN 'derivative.resize' THEN 'resize' ELSE 'sharpen' END)
 OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1' THEN RETURN false;END IF;
 -- Physical NPY/geometry checks are performed by the scheduler verifier before
 -- this trusted function; SQL rechecks exact producer and byte attestations.
 marker:=photo.v2_adoption_event_id(t.id,t.canonical_result_id);
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
 VALUES(e.tenant_id,'v30-stage-verified',marker,clock_timestamp()) ON CONFLICT DO NOTHING;
 IF e.expansion_lease_until<=clock_timestamp() THEN
  RAISE EXCEPTION 'DELIVERY_STAGE_VERIFICATION_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.record_v30_delivery_stage_verification(uuid,uuid,integer,text,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v30_delivery_stage_verification(uuid,uuid,integer,text,jsonb,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v30_delivery_stage_verification(uuid,uuid,integer,text,jsonb,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
