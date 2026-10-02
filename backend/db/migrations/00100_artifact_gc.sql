-- +goose Up
ALTER TABLE photo.artifacts ADD COLUMN gc_token uuid;
ALTER TABLE photo.artifacts ADD COLUMN gc_lease_until timestamptz;
CREATE INDEX artifacts_gc_ready_idx ON photo.artifacts(expires_at,id) WHERE state='ready' AND expires_at IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION photo.artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ SELECT
 EXISTS(SELECT FROM photo.artifact_leases r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact AND r.expires_at>clock_timestamp()) OR
 EXISTS(SELECT FROM photo.assets r WHERE r.tenant_id=p_tenant AND r.original_artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.asset_versions r WHERE r.tenant_id=p_tenant AND r.master_artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.upload_sessions r WHERE r.tenant_id=p_tenant AND r.staging_artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.style_versions r WHERE r.tenant_id=p_tenant AND r.export_artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.style_asset_bindings r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.training_pairs r WHERE r.tenant_id=p_tenant AND r.ignore_mask_artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.snapshots r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.snapshot_items r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.tasks r WHERE r.tenant_id=p_tenant AND r.params_artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.reviews r WHERE r.tenant_id=p_tenant AND (r.candidate_artifact_id=p_artifact OR r.base_artifact_id=p_artifact)) OR
 EXISTS(SELECT FROM photo.derivatives r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.exports r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.download_job_items r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.download_volumes r WHERE r.tenant_id=p_tenant AND (r.artifact_id=p_artifact OR r.manifest_artifact_id=p_artifact)) OR
 EXISTS(SELECT FROM photo.download_intents r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) $$;
-- +goose StatementEnd
ALTER FUNCTION photo.artifact_has_references(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.artifact_has_references(uuid,uuid) FROM PUBLIC;

-- Claim only truly unreferenced immutable objects. New references require
-- state=ready and therefore cannot attach after this row changes to deleting.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_artifact_gc()
RETURNS TABLE(artifact_id uuid,gc_token uuid,tenant_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE a photo.artifacts%ROWTYPE;token uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO a FROM photo.artifacts x WHERE x.state='ready' AND x.expires_at<=clock_timestamp()
 AND x.object_version IS NOT NULL AND x.object_version NOT IN ('','null')
 AND NOT EXISTS(SELECT FROM photo.artifact_leases l WHERE l.tenant_id=x.tenant_id AND l.artifact_id=x.id AND l.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT FROM photo.assets r WHERE r.tenant_id=x.tenant_id AND r.original_artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.asset_versions r WHERE r.tenant_id=x.tenant_id AND r.master_artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.upload_sessions r WHERE r.tenant_id=x.tenant_id AND r.staging_artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.style_versions r WHERE r.tenant_id=x.tenant_id AND r.export_artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.style_asset_bindings r WHERE r.tenant_id=x.tenant_id AND r.artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.training_pairs r WHERE r.tenant_id=x.tenant_id AND r.ignore_mask_artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.snapshots r WHERE r.tenant_id=x.tenant_id AND r.artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.snapshot_items r WHERE r.tenant_id=x.tenant_id AND r.artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.tasks r WHERE r.tenant_id=x.tenant_id AND r.params_artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.reviews r WHERE r.tenant_id=x.tenant_id AND (r.candidate_artifact_id=x.id OR r.base_artifact_id=x.id))
 AND NOT EXISTS(SELECT FROM photo.derivatives r WHERE r.tenant_id=x.tenant_id AND r.artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.exports r WHERE r.tenant_id=x.tenant_id AND r.artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.download_job_items r WHERE r.tenant_id=x.tenant_id AND r.artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.download_volumes r WHERE r.tenant_id=x.tenant_id AND (r.artifact_id=x.id OR r.manifest_artifact_id=x.id))
 AND NOT EXISTS(SELECT FROM photo.download_intents r WHERE r.tenant_id=x.tenant_id AND r.artifact_id=x.id)
 ORDER BY x.expires_at,x.id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.artifacts SET state='deleting',gc_token=token,gc_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=a.id;
 RETURN QUERY SELECT a.id,token,a.tenant_id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_artifact_gc() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_artifact_gc() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_artifact_gc() TO photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.finish_artifact_gc(p_artifact uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO a FROM photo.artifacts WHERE id=p_artifact FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF a.state='deleted' THEN RETURN a.gc_token=p_token;END IF;
 IF a.state<>'deleting' OR a.gc_token IS DISTINCT FROM p_token OR a.gc_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF photo.artifact_has_references(a.tenant_id,a.id) THEN RAISE EXCEPTION 'ARTIFACT_GC_REFERENCE_RACE' USING ERRCODE='40001';END IF;
 UPDATE photo.artifacts SET state='deleted',gc_lease_until=NULL,verified_at=NULL WHERE id=a.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_artifact_gc(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_artifact_gc(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_artifact_gc(uuid,uuid) TO photo_maintenance;

-- Recover a crashed claimant without allowing a different token to finish it.
-- +goose StatementBegin
CREATE FUNCTION photo.recover_artifact_gc() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE changed integer;
BEGIN
 UPDATE photo.artifacts SET state='ready',gc_token=NULL,gc_lease_until=NULL WHERE state='deleting' AND gc_lease_until<=clock_timestamp();
 GET DIAGNOSTICS changed=ROW_COUNT;RETURN changed;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.recover_artifact_gc() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.recover_artifact_gc() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.recover_artifact_gc() TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
