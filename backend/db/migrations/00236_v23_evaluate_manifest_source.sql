-- Expose only the immutable manifest object pinned by a live evaluation lease.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_manifest_source(p_tenant uuid,p_run uuid,
 p_expansion uuid,p_token uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL THEN RETURN; END IF;
 RETURN QUERY SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,
  a.object_version,a.media_type
 FROM photo.graph_expansions e
 JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id
  AND i.snapshot_id=r.input_snapshot_id AND i.ordinal=0
 JOIN photo.backend_documents d ON d.tenant_id=r.tenant_id
  AND d.run_id=r.id AND d.artifact_id=i.artifact_id
  AND d.owner_id=(r.config_snapshot->'v23_training'->>'profile_id')::uuid
  AND d.purpose='v23_training_manifest' AND d.state='ready'
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id
  AND a.id=d.artifact_id AND a.sha256=d.sha256
  AND a.object_version=d.object_version
 WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_expansion
  AND e.kind='v2.learning.evaluate/1' AND e.state='open'
  AND e.expansion_token=p_token
  AND e.expansion_lease_until>clock_timestamp()
  AND r.kind='v2' AND r.status='running' AND NOT r.graph_sealed
  AND NOT r.cancel_requested AND w.workflow_type='learning'
  AND i.sha256=d.sha256
  AND d.sha256=r.config_snapshot->'v23_training'->>'manifest_sha'
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.media_type='application/json';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_manifest_source(uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_manifest_source(uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_manifest_source(uuid,uuid,uuid,uuid)
 TO photo_scheduler;
