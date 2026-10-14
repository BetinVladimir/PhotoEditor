-- +goose Up
-- Preserve full current fairness/resource/model/signer dispatch definitions.
-- Schema drift fails rather than installing an older copied dispatcher.
-- +goose StatementBegin
DO $$
DECLARE signature text; definition text; expected integer; occurrences integer;
 needle text:='w.last_seen>clock_timestamp()';
 gate text:='(w.capacity_policy_sha IS NULL OR (w.reported_capacity_policy_sha=w.capacity_policy_sha AND w.capacity_policy_reported_at>clock_timestamp()-interval ''30 seconds'' AND w.capacity_policy_reported_at<=clock_timestamp())) AND ';
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.eligible_dispatch_nodes(uuid)',
 'photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)',
 'photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)',
 'photo.select_style_analysis_runtimes(uuid,boolean)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  expected:=CASE WHEN signature='photo.select_style_analysis_runtimes(uuid,boolean)' THEN 2 ELSE 1 END;
  occurrences:=(length(definition)-length(replace(definition,needle,'')))/length(needle);
  IF occurrences<>expected OR position('w.capacity_policy_sha' IN definition)>0 THEN
   RAISE EXCEPTION 'Unexpected capacity policy dispatch definition: %',signature;
  END IF;
  EXECUTE replace(definition,needle,gate||needle);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
