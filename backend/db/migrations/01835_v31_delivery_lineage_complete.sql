-- +goose Up
-- Completion half of the delegated V31 lineage publication protocol. The
-- parent lease, exact export pins and reservation token are rechecked after
-- immutable object upload, in the same tenant-usage locking order as V31.
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v31_delivery_lineage(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_expected jsonb,
 p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text,
 p_document_token uuid,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE delegation jsonb;tenant uuid;export_id uuid;source_run uuid;
 d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_document_token IS NULL OR p_version IS NULL OR p_version IN('','null')
  OR length(p_version)>1024 THEN RETURN false;END IF;
 delegation:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF delegation IS NULL THEN RETURN false;END IF;
 tenant:=(delegation->>'tenant_id')::uuid;
 export_id:=(delegation->>'export_id')::uuid;
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_artifact IS NULL
  OR p_artifact IS DISTINCT FROM photo.v30_uuid5(export_id,'signing:lineage:'||p_sha)
  OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 8388608
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 source_run:=photo.v31_delivery_lineage_scope(p_expansion,p_token,p_ordinal,
  p_preset,p_expected,p_bucket);
 IF source_run IS NULL THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=tenant
  AND b.run_id=source_run AND b.owner_id=export_id
  AND b.purpose='v31_signing_lineage' FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact
  OR d.token IS DISTINCT FROM p_document_token OR d.sha256 IS DISTINCT FROM p_sha
  OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket
  THEN RETURN false;END IF;
 IF d.state='ready' THEN
  IF d.object_version IS DISTINCT FROM p_version THEN RETURN false;END IF;
  PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=tenant
   AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256
   AND a.bytes=d.bytes AND a.bucket=d.bucket
   AND a.object_key='tenants/'||tenant||'/runs/'||source_run||'/documents/'||d.artifact_id
   AND a.media_type='application/json' AND a.output_role='v31_signing_lineage'
   AND a.object_version=p_version AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
  RETURN FOUND;
 END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,
  sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,tenant,d.bucket,
  'tenants/'||tenant||'/runs/'||source_run||'/documents/'||d.artifact_id,
  p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),
  'v31_signing_lineage');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,
  storage_bytes=storage_bytes+d.bytes
  WHERE tenant_id=tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,
  completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.complete_v31_delivery_lineage(uuid,uuid,integer,uuid,jsonb,uuid,text,bigint,text,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
