-- +goose Up
-- A NULL token hash must return a normal refusal before the INSERT rather
-- than pass a NULL-valued IF expression and raise a NOT NULL violation.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.create_v30_delivery_download_intent(
 p_tenant uuid,p_actor uuid,p_session uuid,p_package uuid,p_kind text,
 p_ordinal integer,p_preset uuid,p_bucket text,p_intent uuid,
 p_token_hash bytea,p_expires timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE target record;
BEGIN
 IF p_intent IS NULL OR p_intent='00000000-0000-0000-0000-000000000000'::uuid
  OR p_actor IS NULL OR p_session IS NULL
  OR p_token_hash IS NULL OR octet_length(p_token_hash)<>32
  OR p_expires IS NULL OR p_expires<=clock_timestamp()
  OR p_expires>clock_timestamp()+interval '60 seconds' THEN RETURN false;END IF;
 SELECT * INTO target FROM photo.v30_delivery_download_target(p_tenant,p_actor,
  p_session,p_package,p_kind,p_ordinal,p_preset,p_bucket);
 IF NOT FOUND OR target.artifact_id IS NULL OR target.object_version IS NULL
  OR target.object_version IN('','null') THEN RETURN false;END IF;
 INSERT INTO photo.download_intents(id,tenant_id,principal_type,principal_id,
  artifact_id,token_hash,expires_at,intent_kind,delivery_package_id,
  delivery_kind,delivery_ordinal,delivery_preset_id,delivery_session_id,
  delivery_sha,delivery_object_version)
 VALUES(p_intent,p_tenant,'user',p_actor,target.artifact_id,p_token_hash,
  p_expires,'delivery',p_package,p_kind,p_ordinal,p_preset,p_session,
  target.sha256,target.object_version);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_v30_delivery_download_intent(uuid,uuid,uuid,uuid,text,integer,uuid,text,uuid,bytea,timestamptz)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_delivery_download_intent(uuid,uuid,uuid,uuid,text,integer,uuid,text,uuid,bytea,timestamptz)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.create_v30_delivery_download_intent(uuid,uuid,uuid,uuid,text,integer,uuid,text,uuid,bytea,timestamptz)
 TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
