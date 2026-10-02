-- +goose Up
-- Read published FrozenRecipe documents from the current trusted publisher.
-- Source identity/geometry must additionally match the verified JSON bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_recipe(p_tenant uuid,p_asset uuid,p_version uuid,p_actor uuid,p_session uuid,p_recipe uuid,p_sha text)
RETURNS TABLE(project_id uuid,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT s.project_id,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v26_region_source(p_tenant,p_asset,p_version,p_actor,p_session) s
 JOIN photo.v2_workflows w ON w.tenant_id=p_tenant AND w.project_id=s.project_id AND w.workflow_type='learning'
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
 JOIN photo.backend_documents d ON d.tenant_id=r.tenant_id AND d.run_id=r.id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  AND a.sha256=d.sha256 AND a.object_version=d.object_version
 WHERE coalesce(p_sha,'') ~ '^[0-9a-f]{64}$' AND d.artifact_id=p_recipe AND d.sha256=p_sha
 AND d.purpose IN('v23_frozen_base','v23_frozen_approved') AND d.state='ready'
 AND a.output_role='v23_frozen_recipe' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_region_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_api,photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
