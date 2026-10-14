-- +goose Up
-- Read/lock admission authority only. No workflow creation or SDK qualification.
-- Caller must hold quota/ledger locks first when composing with mutation.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_admission_ready(p_tenant uuid,p_actor uuid,p_session uuid,p_export uuid,p_expected jsonb,p_bucket text,p_params text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;params jsonb;lineage uuid;policy uuid;source uuid;a photo.artifacts%ROWTYPE;aid uuid;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 BEGIN params:=p_params::jsonb;EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(params))<>5 THEN RETURN false;END IF;
 IF params->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(params->'signing_policy_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'export') IS DISTINCT FROM 'object' OR jsonb_typeof(params->'lineage') IS DISTINCT FROM 'object'
 OR jsonb_typeof(params->'policy') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(params->'export'))<>2
 OR (SELECT count(*) FROM jsonb_object_keys(params->'lineage'))<>2
 OR (SELECT count(*) FROM jsonb_object_keys(params->'policy'))<>2
 OR jsonb_typeof(params->'export'->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'export'->'sha256') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'lineage'->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'lineage'->'sha256') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'policy'->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'policy'->'sha256') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 BEGIN
  source:=(params->'export'->>'artifact_id')::uuid;lineage:=(params->'lineage'->>'artifact_id')::uuid;policy:=(params->'policy'->>'artifact_id')::uuid;
  IF (params->>'signing_policy_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF source IS NULL OR lineage IS NULL OR policy IS NULL OR source=lineage OR source=policy OR lineage=policy
 OR params->'export'->>'artifact_id' IS DISTINCT FROM p_expected->>'artifact'
 OR params->'export'->>'sha256' IS DISTINCT FROM p_expected->>'sha256'
 OR params->'lineage'->>'sha256' IS NULL OR params->'lineage'->>'sha256' !~ '^[0-9a-f]{64}$'
 OR params->'policy'->>'sha256' IS NULL OR params->'policy'->>'sha256' !~ '^[0-9a-f]{64}$'
 OR lineage IS DISTINCT FROM photo.v30_uuid5(p_export,'signing:lineage:'||(params->'lineage'->>'sha256')) THEN RETURN false;END IF;
 rid:=photo.v31_signing_lineage_scope(p_tenant,p_actor,p_session,p_export,p_expected,p_bucket);IF rid IS NULL THEN RETURN false;END IF;
 -- Stable lock order; artifact GC changes ready->deleting under conflicting locks.
 FOR aid IN SELECT id FROM unnest(ARRAY[lineage,policy]) id ORDER BY id LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=aid FOR SHARE;
  IF NOT FOUND OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.bucket IS DISTINCT FROM p_bucket
  OR coalesce(a.object_version,'') IN('','null') OR a.object_key='' OR a.media_type IS DISTINCT FROM 'application/json'
  OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
  IF aid=lineage THEN
   IF a.sha256 IS DISTINCT FROM params->'lineage'->>'sha256' OR a.bytes NOT BETWEEN 2 AND 8388608
   OR a.output_role IS DISTINCT FROM 'v31_signing_lineage' THEN RETURN false;END IF;
   PERFORM d.id FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=rid AND d.owner_id=p_export
   AND d.purpose='v31_signing_lineage' AND d.state='ready' AND d.artifact_id=a.id AND d.sha256=a.sha256
   AND d.bytes=a.bytes AND d.bucket=a.bucket AND d.object_version=a.object_version
   AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||a.id FOR SHARE;
   IF NOT FOUND THEN RETURN false;END IF;
  ELSE
   IF a.sha256 IS DISTINCT FROM params->'policy'->>'sha256' OR a.bytes NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
  END IF;
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
