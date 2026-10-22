-- +goose Up
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends)
VALUES('series.pack','1','cpu','cpu.series_pack',NULL,300,'["cpu"]');
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
VALUES('series.pack','1','series_stats_pack','["application/json"]',1048576,1);
UPDATE photo.operation_registry SET timeout_seconds=7200 WHERE operation='series.balance' AND version='1';
UPDATE photo.operation_output_policies SET max_bytes=8388608 WHERE operation='series.balance' AND version='1' AND role='series_corrections';
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text:='allowed constant text[]:=ARRAY[';
BEGIN
 definition:=pg_get_functiondef('photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb)'::regprocedure);
 IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'SERIES_PACK_PLAN_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,needle,needle||'''series.pack'',');
 ALTER FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
 REVOKE ALL ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
 GRANT EXECUTE ON FUNCTION photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
