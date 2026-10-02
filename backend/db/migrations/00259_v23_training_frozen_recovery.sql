-- Persist exact FrozenRecipe bytes atomically with the pending draft before
-- object IO. The final 202 ticket replaces these recovery bytes at seal.
-- +goose Up
ALTER FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 RENAME TO save_v23_training_recovery_core;
REVOKE ALL ON FUNCTION photo.save_v23_training_recovery_core(uuid,uuid,uuid,uuid,text,text,jsonb)
 FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.save_v23_training_recovery(p_tenant uuid,p_actor uuid,
 p_auth_session uuid,p_run uuid,p_key text,p_request_sha text,p_body jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE base jsonb; frozen_text text; frozen jsonb; examples jsonb;
 item jsonb; example jsonb; n integer; i integer;
 idem photo.idempotency_records%ROWTYPE; saved boolean;
BEGIN
 IF p_body IS NULL OR jsonb_typeof(p_body)<>'object' THEN
  RAISE EXCEPTION 'V23_TRAIN_RECOVERY_INVALID' USING ERRCODE='22023'; END IF;
 IF NOT p_body ? 'frozen_jcs' THEN
  RETURN photo.save_v23_training_recovery_core(p_tenant,p_actor,
   p_auth_session,p_run,p_key,p_request_sha,p_body);
 END IF;
 IF jsonb_typeof(p_body->'frozen_jcs')<>'string' OR
  octet_length(p_body::text)>33554432 THEN
  RAISE EXCEPTION 'V23_TRAIN_FROZEN_RECOVERY_INVALID'
   USING ERRCODE='22023'; END IF;
 frozen_text:=p_body->>'frozen_jcs';
 IF octet_length(frozen_text) NOT BETWEEN 2 AND 16777216 THEN
  RAISE EXCEPTION 'V23_TRAIN_FROZEN_RECOVERY_INVALID'
   USING ERRCODE='22023'; END IF;
 frozen:=frozen_text::jsonb;
 examples:=(p_body->>'manifest_jcs')::jsonb->'examples';
 IF jsonb_typeof(frozen)<>'array' OR
  jsonb_typeof(examples)<>'array' OR
  jsonb_array_length(frozen) NOT BETWEEN 40 AND 5000 OR
  jsonb_array_length(frozen)<>jsonb_array_length(examples) THEN
  RAISE EXCEPTION 'V23_TRAIN_FROZEN_RECOVERY_INVALID'
   USING ERRCODE='22023'; END IF;
 n:=jsonb_array_length(frozen);
 FOR i IN 0..n-1 LOOP
  item:=frozen->i;
  example:=examples->i;
  IF jsonb_typeof(item)<>'object' OR
   (item-'event_id'-'base_jcs'-'approved_jcs')<>'{}'::jsonb OR
   item->>'event_id' IS DISTINCT FROM example->>'event_id' OR
   jsonb_typeof(item->'base_jcs')<>'string' OR
   jsonb_typeof(item->'approved_jcs')<>'string' OR
   octet_length(item->>'base_jcs') NOT BETWEEN 2 AND 1048576 OR
   octet_length(item->>'approved_jcs') NOT BETWEEN 2 AND 1048576 OR
   encode(public.digest(convert_to(item->>'base_jcs','UTF8'),'sha256'),
    'hex') IS DISTINCT FROM
    example->'render_case'->'base_recipe'->>'sha256' OR
   encode(public.digest(convert_to(item->>'approved_jcs','UTF8'),'sha256'),
    'hex') IS DISTINCT FROM
    example->'render_case'->'approved_recipe'->>'sha256' THEN
   RAISE EXCEPTION 'V23_TRAIN_FROZEN_RECOVERY_INVALID'
    USING ERRCODE='23514'; END IF;
 END LOOP;
 base:=p_body-'frozen_jcs';
 SELECT * INTO idem FROM photo.idempotency_records x
  WHERE x.tenant_id=p_tenant AND x.principal_type='user'
   AND x.principal_id=p_actor AND x.route='V2.Admit'
   AND x.key=p_key AND x.request_hash=p_request_sha FOR UPDATE;
 IF idem.id IS NOT NULL AND idem.response_status IS NULL AND
  idem.response_body=p_body THEN
  UPDATE photo.idempotency_records SET response_body=base WHERE id=idem.id;
 END IF;
 saved:=photo.save_v23_training_recovery_core(p_tenant,p_actor,
  p_auth_session,p_run,p_key,p_request_sha,base);
 IF NOT saved THEN RETURN false; END IF;
 UPDATE photo.idempotency_records SET response_body=p_body
  WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
   AND route='V2.Admit' AND key=p_key AND request_hash=p_request_sha
   AND response_status IS NULL AND response_body=base;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'V23_TRAIN_FROZEN_RECOVERY_CONFLICT'
   USING ERRCODE='23514'; END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
