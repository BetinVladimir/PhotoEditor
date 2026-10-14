-- Retain exact portrait.detail evidence bytes in a pending training receipt.
-- The prior reviewed-holdout validator remains the authority for its fields.
-- +goose Up
ALTER FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 RENAME TO save_v23_training_recovery_holdout_core;
REVOKE ALL ON FUNCTION photo.save_v23_training_recovery_holdout_core(
 uuid,uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.save_v23_training_recovery(p_tenant uuid,p_actor uuid,
 p_auth_session uuid,p_run uuid,p_key text,p_request_sha text,p_body jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE base jsonb; examples jsonb; items jsonb; item jsonb; evidence jsonb;
 example jsonb; seen uuid[]:=ARRAY[]::uuid[]; event_id uuid;
 aid uuid; h bytea; body_text text; body_sha text;
 idem photo.idempotency_records%ROWTYPE; saved boolean;
BEGIN
 IF p_body IS NULL OR jsonb_typeof(p_body)<>'object' OR
  octet_length(p_body::text)>33554432 THEN
  RAISE EXCEPTION 'V23_DETAIL_RECOVERY_INVALID' USING ERRCODE='22023';
 END IF;
 IF NOT p_body ? 'detail_jcs' THEN
  IF jsonb_typeof(p_body->'manifest_jcs') IS DISTINCT FROM 'string' THEN
   RAISE EXCEPTION 'V23_DETAIL_RECOVERY_INVALID' USING ERRCODE='22023';
  END IF;
  examples:=(p_body->>'manifest_jcs')::jsonb->'examples';
  IF jsonb_typeof(examples) IS DISTINCT FROM 'array' OR EXISTS(
   SELECT 1 FROM jsonb_array_elements(examples) x WHERE
    x.value->'render_case' ? 'detail_evidence') THEN
   RAISE EXCEPTION 'V23_DETAIL_RECOVERY_MISSING' USING ERRCODE='23514';
  END IF;
  RETURN photo.save_v23_training_recovery_holdout_core(p_tenant,p_actor,
   p_auth_session,p_run,p_key,p_request_sha,p_body);
 END IF;
 IF jsonb_typeof(p_body->'manifest_jcs') IS DISTINCT FROM 'string' OR
  jsonb_typeof(p_body->'detail_jcs') IS DISTINCT FROM 'string' OR
  octet_length(p_body->>'detail_jcs') NOT BETWEEN 2 AND 16777216 THEN
  RAISE EXCEPTION 'V23_DETAIL_RECOVERY_INVALID' USING ERRCODE='22023';
 END IF;
 examples:=(p_body->>'manifest_jcs')::jsonb->'examples';
 items:=(p_body->>'detail_jcs')::jsonb;
 IF jsonb_typeof(examples) IS DISTINCT FROM 'array' OR
  jsonb_array_length(examples) NOT BETWEEN 40 AND 5000 OR
  jsonb_typeof(items) IS DISTINCT FROM 'array' OR
  jsonb_array_length(items) NOT BETWEEN 1 AND 5000 THEN
  RAISE EXCEPTION 'V23_DETAIL_RECOVERY_INVALID' USING ERRCODE='22023';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(items) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR
   (item-'event_id'-'body_jcs')<>'{}'::jsonb OR
   jsonb_typeof(item->'event_id') IS DISTINCT FROM 'string' OR
   jsonb_typeof(item->'body_jcs') IS DISTINCT FROM 'string' OR
   octet_length(item->>'body_jcs') NOT BETWEEN 2 AND 1048576 THEN
   RAISE EXCEPTION 'V23_DETAIL_RECOVERY_INVALID' USING ERRCODE='22023';
  END IF;
  event_id:=(item->>'event_id')::uuid;
  IF event_id=ANY(seen) THEN
   RAISE EXCEPTION 'V23_DETAIL_RECOVERY_DUPLICATE' USING ERRCODE='23514';
  END IF;
  seen:=array_append(seen,event_id);
  body_text:=item->>'body_jcs';
  evidence:=body_text::jsonb;
  body_sha:=encode(public.digest(convert_to(body_text,'UTF8'),'sha256'),'hex');
  h:=public.digest(uuid_send(p_run)||convert_to(
   'v23-detail/'||event_id::text,'UTF8'),'sha1');
  h:=set_byte(h,6,(get_byte(h,6)&15)|80);
  h:=set_byte(h,8,(get_byte(h,8)&63)|128);
  aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
  SELECT value INTO example FROM jsonb_array_elements(examples)
   WHERE value->>'event_id'=event_id::text;
  IF NOT FOUND OR
   (SELECT count(*) FROM jsonb_array_elements(examples) x WHERE
    x.value->>'event_id'=event_id::text)<>1 OR
   jsonb_typeof(evidence) IS DISTINCT FROM 'object' OR
   evidence->>'tenant_id' IS DISTINCT FROM p_tenant::text OR
   evidence->>'event_id' IS DISTINCT FROM event_id::text OR
   evidence->>'asset_version_id' IS DISTINCT FROM
    example->'render_case'->'binding'->>'asset_version_id' OR
   evidence->'source' IS DISTINCT FROM
    example->'render_case'->'working_image' OR
   evidence->'protection' IS DISTINCT FROM
    example->'render_case'->'protected' OR
   example->'render_case'->'detail_evidence'->>'artifact_id'
    IS DISTINCT FROM aid::text OR
   example->'render_case'->'detail_evidence'->>'sha256'
    IS DISTINCT FROM body_sha OR
   example->'render_case' ? 'defect_evidence' OR
   example->'render_case' ? 'dust_evidence' THEN
   RAISE EXCEPTION 'V23_DETAIL_RECOVERY_CONFLICT' USING ERRCODE='23514';
  END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(examples) x WHERE
  x.value->'render_case' ? 'detail_evidence' AND
  NOT ((x.value->>'event_id')::uuid=ANY(seen))) THEN
  RAISE EXCEPTION 'V23_DETAIL_RECOVERY_MISSING' USING ERRCODE='23514';
 END IF;
 base:=p_body-'detail_jcs';
 SELECT * INTO idem FROM photo.idempotency_records x WHERE
  x.tenant_id=p_tenant AND x.principal_type='user' AND
  x.principal_id=p_actor AND x.route='V2.Admit' AND x.key=p_key AND
  x.request_hash=p_request_sha FOR UPDATE;
 IF idem.id IS NOT NULL AND idem.response_status IS NULL AND
  idem.response_body=p_body THEN
  UPDATE photo.idempotency_records SET response_body=base WHERE id=idem.id;
 END IF;
 saved:=photo.save_v23_training_recovery_holdout_core(p_tenant,p_actor,
  p_auth_session,p_run,p_key,p_request_sha,base);
 IF NOT saved THEN RETURN false; END IF;
 UPDATE photo.idempotency_records SET response_body=p_body WHERE
  tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND
  route='V2.Admit' AND key=p_key AND request_hash=p_request_sha AND
  response_status IS NULL AND response_body=base;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'V23_DETAIL_RECOVERY_CONFLICT' USING ERRCODE='23514';
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v23_training_recovery(
 uuid,uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v23_training_recovery(
 uuid,uuid,uuid,uuid,text,text,jsonb) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
