-- +goose Up
-- Ready receipt for the exact delegated lineage. A source/export or parent
-- lease change makes the receipt unusable before child compilation/admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_lineage_ready(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_expected jsonb,
 p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE delegation jsonb;tenant uuid;export_id uuid;source_run uuid;
BEGIN
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_artifact IS NULL
  OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 8388608
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false;END IF;
 delegation:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF delegation IS NULL THEN RETURN false;END IF;
 tenant:=(delegation->>'tenant_id')::uuid;
 export_id:=(delegation->>'export_id')::uuid;
 IF p_artifact IS DISTINCT FROM photo.v30_uuid5(export_id,'signing:lineage:'||p_sha)
  THEN RETURN false;END IF;
 source_run:=photo.v31_delivery_lineage_scope(p_expansion,p_token,p_ordinal,
  p_preset,p_expected,p_bucket);
 IF source_run IS NULL THEN RETURN false;END IF;
 PERFORM d.id FROM photo.backend_documents d JOIN photo.artifacts a
  ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE d.tenant_id=tenant AND d.run_id=source_run AND d.owner_id=export_id
   AND d.purpose='v31_signing_lineage' AND d.state='ready'
   AND d.artifact_id=p_artifact AND d.sha256=p_sha AND d.bytes=p_bytes
   AND d.bucket=p_bucket AND d.object_version NOT IN('','null')
   AND a.state='ready' AND a.sha256=p_sha AND a.bytes=p_bytes
   AND a.bucket=p_bucket AND a.media_type='application/json'
   AND a.output_role='v31_signing_lineage'
   AND a.object_key='tenants/'||tenant||'/runs/'||source_run||'/documents/'||p_artifact
   AND a.object_version=d.object_version AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  FOR SHARE OF d,a;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_lineage_ready(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_lineage_ready(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_lineage_ready(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
