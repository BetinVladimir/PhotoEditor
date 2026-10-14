-- Fit validates and pins every example in the training manifest. Evaluate
-- replays only examples whose split is holdout. Require every Evaluate
-- evidence input to be a Fit-declared, event-owned, live-authorized input;
-- requiring every Fit evidence input would also require train examples and
-- make a normal mixed training set impossible to evaluate.
-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE definition text; function_name text; first_gate text;
 second_gate text; start_at integer; end_at integer;
BEGIN
 first_gate:='IF EXISTS(SELECT 1 FROM photo.graph_expansions e';
 second_gate:='OR EXISTS(SELECT 1 FROM photo.v2_task_inputs ei';
 FOREACH function_name IN ARRAY ARRAY[
  'v23_evaluate_defect_closure_current',
  'v23_evaluate_dust_closure_current'] LOOP
  definition:=pg_get_functiondef(format(
   'photo.%I(uuid,uuid,uuid,uuid,uuid)',function_name)::regprocedure);
  IF (length(definition)-length(replace(definition,first_gate,''))) /
   length(first_gate)<>1 OR
   (length(definition)-length(replace(definition,second_gate,''))) /
   length(second_gate)<>1 THEN
   RAISE EXCEPTION 'Unexpected V23 Evaluate evidence gate: %',function_name;
  END IF;
  start_at:=strpos(definition,first_gate);
  end_at:=strpos(definition,second_gate);
  IF start_at<1 OR end_at<=start_at THEN
   RAISE EXCEPTION 'Unexpected V23 Evaluate evidence order: %',function_name;
  END IF;
  definition:=substring(definition FROM 1 FOR start_at-1)||
   'IF '||substring(definition FROM end_at+3);
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_defect_closure_current(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v23_evaluate_dust_closure_current(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_defect_closure_current(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION photo.v23_evaluate_dust_closure_current(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_defect_closure_current(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_dust_closure_current(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
