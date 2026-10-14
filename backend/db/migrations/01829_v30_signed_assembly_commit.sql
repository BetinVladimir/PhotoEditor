-- +goose Up
-- Give signed and ordinary Delivery the same atomic package/run/event
-- transition, but with disjoint manifest authority. The exact predecessor
-- definition is guarded so an unrelated change cannot silently be copied.
-- +goose StatementBegin
DO $$ DECLARE definition text;ordinary text:='photo.adopt_v30_delivery_assembled(';
 signed text:='photo.adopt_v30_delivery_signed_assembled(';
 ordinary_authority text:='photo.v30_delivery_manifest_authority(';
 signed_authority text:='photo.v30_delivery_signed_manifest_authority(';
BEGIN
 SELECT pg_get_functiondef('photo.adopt_v30_delivery_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)'::regprocedure)
  INTO definition;
 IF definition IS NULL OR position(ordinary IN definition)=0
  OR position(ordinary_authority IN definition)=0
  OR position('adopt_v2_plan_pre_v30_assembly' IN definition)=0
  OR position('authority->''shards_jcs''' IN definition)=0
  OR position('delivery.assembled' IN definition)=0
  OR position(signed IN definition)>0 THEN
  RAISE EXCEPTION 'DELIVERY_SIGNED_COMMIT_SOURCE_SHAPE';END IF;
 definition:=replace(definition,ordinary,signed);
 definition:=replace(definition,ordinary_authority,signed_authority);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_v30_delivery_signed_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_delivery_signed_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_delivery_signed_assembled(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
