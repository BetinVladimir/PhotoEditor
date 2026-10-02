-- Merge after 00196_gc_refs.sql. Existing GC lock/detach semantics preserved.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ SELECT
 EXISTS(SELECT FROM photo.artifact_leases r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact AND r.expires_at>clock_timestamp()) OR
 EXISTS(SELECT FROM photo.assets r WHERE r.tenant_id=p_tenant AND r.original_artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.asset_versions r WHERE r.tenant_id=p_tenant AND r.master_artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.upload_sessions r WHERE r.tenant_id=p_tenant AND r.staging_artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.style_versions r WHERE r.tenant_id=p_tenant AND r.export_artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.style_asset_bindings r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.training_pairs r WHERE r.tenant_id=p_tenant AND r.ignore_mask_artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.snapshots r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.snapshot_items r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.tasks r WHERE r.tenant_id=p_tenant AND r.params_artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.reviews r WHERE r.tenant_id=p_tenant AND (r.candidate_artifact_id=p_artifact OR r.base_artifact_id=p_artifact)) OR
 EXISTS(SELECT FROM photo.derivatives r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.exports r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.download_job_items r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR EXISTS(SELECT FROM photo.download_volumes r WHERE r.tenant_id=p_tenant AND (r.artifact_id=p_artifact OR r.manifest_artifact_id=p_artifact)) OR
 EXISTS(SELECT FROM photo.download_intents r WHERE r.tenant_id=p_tenant AND r.artifact_id=p_artifact) OR photo.v2_artifact_has_references(p_tenant,p_artifact) $$;
-- +goose StatementEnd
ALTER FUNCTION photo.artifact_has_references(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.artifact_has_references(uuid,uuid) FROM PUBLIC;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_artifact_gc()
RETURNS TABLE(artifact_id uuid,gc_token uuid,tenant_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE a photo.artifacts%ROWTYPE;token uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO a FROM photo.artifacts x WHERE x.state='ready' AND x.expires_at<=clock_timestamp()
 AND EXISTS(SELECT FROM photo.tenants tenant WHERE tenant.id=x.tenant_id AND tenant.status='active')
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
 AND NOT EXISTS(SELECT FROM photo.download_job_items r JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.id=r.download_job_id WHERE r.tenant_id=x.tenant_id AND r.artifact_id=x.id AND (j.expires_at IS NULL OR j.expires_at>clock_timestamp()))
 AND NOT EXISTS(SELECT FROM photo.download_volumes r JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.id=r.download_job_id WHERE r.tenant_id=x.tenant_id AND (r.artifact_id=x.id OR r.manifest_artifact_id=x.id) AND (j.expires_at IS NULL OR j.expires_at>clock_timestamp()))
 AND NOT photo.v2_artifact_has_references(x.tenant_id,x.id)
 ORDER BY x.expires_at,x.id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.derivatives SET artifact_id=NULL,status='queued',task_id=NULL WHERE tenant_id=a.tenant_id AND artifact_id=a.id;
 UPDATE photo.exports SET artifact_id=NULL,status='queued',task_id=NULL WHERE tenant_id=a.tenant_id AND artifact_id=a.id;
 UPDATE photo.download_jobs j SET status='expired' WHERE j.tenant_id=a.tenant_id AND j.expires_at<=clock_timestamp() AND EXISTS(SELECT FROM photo.download_volumes v WHERE v.tenant_id=j.tenant_id AND v.download_job_id=j.id AND (v.artifact_id=a.id OR v.manifest_artifact_id=a.id));
 DELETE FROM photo.download_intents WHERE tenant_id=a.tenant_id AND artifact_id=a.id;
 UPDATE photo.download_job_items r SET artifact_id=NULL,sha256=NULL,bytes=NULL WHERE r.tenant_id=a.tenant_id AND r.artifact_id=a.id AND EXISTS(SELECT FROM photo.download_jobs j WHERE j.tenant_id=r.tenant_id AND j.id=r.download_job_id AND j.status='expired');
 UPDATE photo.download_volumes r SET artifact_id=NULL,manifest_artifact_id=NULL,status='expired' WHERE r.tenant_id=a.tenant_id AND (r.artifact_id=a.id OR r.manifest_artifact_id=a.id) AND EXISTS(SELECT FROM photo.download_jobs j WHERE j.tenant_id=r.tenant_id AND j.id=r.download_job_id AND j.status='expired');
 IF photo.artifact_has_references(a.tenant_id,a.id) THEN RAISE EXCEPTION 'ARTIFACT_GC_DETACH_INCOMPLETE' USING ERRCODE='40001';END IF;
 UPDATE photo.artifacts SET state='deleting',gc_token=token,gc_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=a.id;
 RETURN QUERY SELECT a.id,token,a.tenant_id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_artifact_gc() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_artifact_gc() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_artifact_gc() TO photo_maintenance;
