-- +goose Up
-- Signed activation has the same fenced parent barrier and receipt as ordinary
-- assembly, but requires the signed transport and current signed matrix.
-- The scheduler must stream/recheck V31 signed objects before this call.
-- +goose StatementBegin
DO $$ DECLARE definition text;
BEGIN
 SELECT pg_get_functiondef('photo.activate_v30_delivery_assembly(uuid,uuid,jsonb,text)'::regprocedure)
  INTO definition;
 IF definition IS NULL
  OR position('photo.v30_delivery_assembly_transport_ready(' IN definition)=0
  OR position('photo.v30_delivery_assembly_source_ready(' IN definition)=0
  OR position('photo.activate_bound_task(' IN definition)=0
  OR position('v30-assembly-activated' IN definition)=0
  OR position('photo.activate_v30_delivery_signed_assembly(' IN definition)>0 THEN
  RAISE EXCEPTION 'DELIVERY_SIGNED_ACTIVATION_SOURCE_SHAPE';END IF;
 definition:=replace(definition,'photo.activate_v30_delivery_assembly(',
  'photo.activate_v30_delivery_signed_assembly(');
 definition:=replace(definition,'photo.v30_delivery_assembly_transport_ready(',
  'photo.v30_delivery_signed_assembly_transport_ready(');
 definition:=replace(definition,'photo.v30_delivery_assembly_source_ready(',
  'photo.v30_delivery_signed_current_matrix(');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.activate_v30_delivery_signed_assembly(uuid,uuid,jsonb,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_delivery_signed_assembly(uuid,uuid,jsonb,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.activate_v30_delivery_signed_assembly(uuid,uuid,jsonb,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
