-- +goose Up
-- Package-scoped one-use user intent. Existing volume/item rows keep their
-- shape; a Delivery token additionally freezes the session and object version
-- so redemption can repeat the live target read before issuing a signed URL.
ALTER TABLE photo.download_intents
 ADD COLUMN delivery_package_id uuid,
 ADD COLUMN delivery_kind text,
 ADD COLUMN delivery_ordinal integer,
 ADD COLUMN delivery_preset_id uuid,
 ADD COLUMN delivery_session_id uuid,
 ADD COLUMN delivery_sha text,
 ADD COLUMN delivery_object_version text;
ALTER TABLE photo.download_intents DROP CONSTRAINT download_intents_kind;
ALTER TABLE photo.download_intents ADD CONSTRAINT download_intents_kind CHECK (
 (intent_kind='volume' AND item_id IS NULL AND preset_id IS NULL
  AND delivery_package_id IS NULL AND delivery_kind IS NULL
  AND delivery_ordinal IS NULL AND delivery_preset_id IS NULL
  AND delivery_session_id IS NULL AND delivery_sha IS NULL
  AND delivery_object_version IS NULL)
 OR (intent_kind='item' AND item_id IS NOT NULL AND preset_id IS NOT NULL
  AND delivery_package_id IS NULL AND delivery_kind IS NULL
  AND delivery_ordinal IS NULL AND delivery_preset_id IS NULL
  AND delivery_session_id IS NULL AND delivery_sha IS NULL
  AND delivery_object_version IS NULL)
 OR (intent_kind='delivery' AND principal_type='user' AND share_id IS NULL
  AND item_id IS NULL AND preset_id IS NULL
  AND delivery_package_id IS NOT NULL
  AND delivery_kind IN('manifest','slot')
  AND ((delivery_kind='manifest' AND delivery_ordinal IS NULL AND delivery_preset_id IS NULL)
   OR (delivery_kind='slot' AND delivery_ordinal BETWEEN 0 AND 4999 AND delivery_preset_id IS NOT NULL))
  AND delivery_session_id IS NOT NULL
  AND delivery_sha IS NOT NULL AND delivery_sha ~ '^[0-9a-f]{64}$'
  AND delivery_object_version IS NOT NULL
  AND delivery_object_version NOT IN('','null')));
ALTER TABLE photo.download_intents ADD CONSTRAINT download_intents_delivery_package_fk
 FOREIGN KEY(tenant_id,delivery_package_id)
 REFERENCES photo.delivery_packages(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

-- +goose StatementBegin
CREATE FUNCTION photo.create_v30_delivery_download_intent(
 p_tenant uuid,p_actor uuid,p_session uuid,p_package uuid,p_kind text,
 p_ordinal integer,p_preset uuid,p_bucket text,p_intent uuid,
 p_token_hash bytea,p_expires timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE target record;
BEGIN
 IF p_intent IS NULL OR p_intent='00000000-0000-0000-0000-000000000000'::uuid
  OR p_actor IS NULL OR p_session IS NULL OR octet_length(p_token_hash)<>32
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
