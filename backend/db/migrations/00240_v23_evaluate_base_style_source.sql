-- Resolve a FrozenRecipe base style through its immutable project style version.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_base_style_source(p_tenant uuid,p_run uuid,
 p_expansion uuid,p_token uuid,p_style uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,
  a.media_type
 FROM photo.graph_expansions e
 JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.styles s ON s.tenant_id=w.tenant_id
  AND s.project_id=w.project_id
 JOIN photo.style_versions version ON version.tenant_id=s.tenant_id
  AND version.style_id=s.id AND version.export_artifact_id=p_style
  AND version.config_hash=p_sha
 JOIN photo.artifacts a ON a.tenant_id=version.tenant_id
  AND a.id=version.export_artifact_id AND a.sha256=version.config_hash
 WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_expansion
  AND e.kind='v2.learning.evaluate/1' AND e.state='open'
  AND e.expansion_token=p_token AND e.expansion_lease_until>clock_timestamp()
  AND r.kind='v2' AND r.status='running' AND NOT r.graph_sealed
  AND NOT r.cancel_requested AND w.workflow_type='learning'
  AND w.renderer_version='v2.2'
  AND version.schema_version='2.0.0'
  AND version.renderer_version='photo_renderer_v2'
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.media_type='application/json'
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_base_style_source(uuid,uuid,uuid,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_base_style_source(uuid,uuid,uuid,uuid,uuid,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_base_style_source(uuid,uuid,uuid,uuid,uuid,text)
 TO photo_scheduler;
