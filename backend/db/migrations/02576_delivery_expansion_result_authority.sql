-- +goose Up
-- Preserve Delivery's expanding (unsealed) parent and sealed CPU child phases.
-- Qualify inbox event identity independently of local event UUID variables.
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;needle text;replacement text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.fail_next_v30_delivery_missing_source(uuid,text)',
  'photo.fail_v30_delivery_physical_source(uuid,text,uuid,uuid,uuid,text,text,text,integer,text,boolean)'
 ] LOOP
  SELECT pg_get_functiondef(p.oid) INTO definition FROM pg_proc p
  WHERE p.oid=to_regprocedure(signature) AND p.prosecdef
   AND pg_get_userbyid(p.proowner)='photo_fn_owner'
   AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql');
  needle:='AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)';
  replacement:='AND photo.inbox.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)';
  IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'DELIVERY_FAILURE_EVENT_SOURCE_SHAPE: %',signature;END IF;
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
 SELECT pg_get_functiondef(p.oid) INTO definition FROM pg_proc p
 WHERE p.oid='photo.delivery_stage_result_outputs_ready(uuid,uuid,integer,bigint,jsonb)'::regprocedure
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql');
 needle:='OR NOT parent.graph_sealed OR parent.created_by IS DISTINCT FROM r.created_by';
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'DELIVERY_PARENT_PHASE_SOURCE_SHAPE';END IF;
 definition:=replace(definition,needle,'OR parent.graph_sealed OR parent.created_by IS DISTINCT FROM r.created_by');
 needle:='OR photo.v2_input_closure_ready(prepare.id) IS DISTINCT FROM true';
 replacement:=$patch$OR NOT EXISTS(SELECT FROM photo.inbox adopted WHERE adopted.tenant_id=p_tenant
  AND adopted.consumer='v2-adopt' AND adopted.event_id=photo.v2_adoption_event_id(prepare.id,prepare.canonical_result_id))
 OR NOT EXISTS(SELECT FROM photo.graph_expansions expansion WHERE expansion.tenant_id=p_tenant
  AND expansion.run_id=parent.id AND expansion.parent_task_id=prepare.id
  AND expansion.kind='v2.delivery/1' AND expansion.state='open')
 OR photo.v2_input_closure_ready(prepare.id) IS DISTINCT FROM true$patch$;
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'DELIVERY_PARENT_ADOPTION_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
