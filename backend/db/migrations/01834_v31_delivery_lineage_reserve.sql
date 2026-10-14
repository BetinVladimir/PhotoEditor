-- +goose Up
-- Reservation half of the delegated V31 lineage publication protocol. The
-- exact parent lease and export pins replace editor-session authority only
-- for this dedicated scheduler entry point.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v31_delivery_lineage(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_expected jsonb,
 p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE delegation jsonb;tenant uuid;export_id uuid;source_run uuid;
 d photo.backend_documents%ROWTYPE;fence uuid:=gen_random_uuid();
 capacity numeric;used numeric;
BEGIN
 delegation:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF delegation IS NULL THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 tenant:=(delegation->>'tenant_id')::uuid;
 export_id:=(delegation->>'export_id')::uuid;
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_artifact IS NULL
  OR p_artifact IS DISTINCT FROM photo.v30_uuid5(export_id,'signing:lineage:'||p_sha)
  OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 8388608
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage
  WHERE tenant_id=tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants
  WHERE id=tenant AND status='active' FOR UPDATE;
 IF used IS NULL OR capacity IS NULL THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 source_run:=photo.v31_delivery_lineage_scope(p_expansion,p_token,p_ordinal,
  p_preset,p_expected,p_bucket);
 IF source_run IS NULL THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=tenant
  AND b.run_id=source_run AND b.owner_id=export_id
  AND b.purpose='v31_signing_lineage' FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>p_artifact OR d.sha256<>p_sha OR d.bytes<>p_bytes
   OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=tenant
    AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes
    AND a.state='ready' AND a.bucket=d.bucket
    AND a.object_key='tenants/'||tenant||'/runs/'||source_run||'/documents/'||d.artifact_id
    AND a.media_type='application/json' AND a.output_role='v31_signing_lineage'
    AND a.object_version=d.object_version AND a.verified_at IS NOT NULL
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
   IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN
   RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence,
   cleanup_after=clock_timestamp()+interval '3 hours' WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',p_artifact,fence,false;RETURN;
 END IF;
 IF used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,
  artifact_id,token,sha256,bytes,bucket)
 VALUES(tenant,source_run,export_id,'v31_signing_lineage',p_artifact,
  fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes
  WHERE tenant_id=tenant;
 RETURN QUERY SELECT 'created',p_artifact,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.reserve_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
