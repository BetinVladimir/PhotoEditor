-- +goose Up
-- Freeze training queue before config hash; recovery records the exact choice.
-- Absent queue in historical receipts remains absent on replay.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v23_training_draft(uuid,uuid,uuid,uuid,bigint,uuid,uuid,text,text,jsonb,bigint,text,text)'::regprocedure);
 needle:='(p_config-''project_id''-''v23_training'')<>''{}''::jsonb';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected training queue begin definition'; END IF;
 EXECUTE replace(definition,needle,'(p_config-''project_id''-''v23_training''-''queue_class'')<>''{}''::jsonb OR
 (p_config ? ''queue_class'' AND p_config->''queue_class'' IS DISTINCT FROM ''"training"''::jsonb)');
 definition:=pg_get_functiondef('photo.save_v23_training_recovery_core(uuid,uuid,uuid,uuid,text,text,jsonb)'::regprocedure);
 needle:='''profile_id''-''project_id'')<>''{}''::jsonb';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected training queue recovery shape'; END IF;
 definition:=replace(definition,needle,'''profile_id''-''project_id''-''queue_class'')<>''{}''::jsonb OR
 (p_body ? ''queue_class'' AND p_body->''queue_class'' IS DISTINCT FROM ''"training"''::jsonb)');
 needle:='r.status<>''draft'' OR r.graph_sealed OR r.cancel_requested OR';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected training queue recovery authority'; END IF;
 EXECUTE replace(definition,needle,needle||'
 r.config_snapshot->''queue_class'' IS DISTINCT FROM p_body->''queue_class'' OR');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
