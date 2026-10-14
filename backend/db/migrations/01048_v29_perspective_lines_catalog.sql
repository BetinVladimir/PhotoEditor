-- +goose Up
-- Immutable source-bound line evidence. No public role may create bindings;
-- a dedicated qualified publisher must be implemented before auto is enabled.
CREATE TABLE photo.v29_perspective_lines (
 tenant_id uuid NOT NULL,
 project_id uuid NOT NULL,
 snapshot_id uuid NOT NULL,
 asset_version_id uuid NOT NULL,
 source_id uuid NOT NULL,
 source_sha text NOT NULL CHECK(source_sha ~ '^[0-9a-f]{64}$'),
 canvas_hash text NOT NULL CHECK(canvas_hash ~ '^[0-9a-f]{64}$'),
 artifact_id uuid NOT NULL,
 sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,artifact_id),
 CHECK(artifact_id<>source_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,source_id) REFERENCES photo.artifacts(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,artifact_id) REFERENCES photo.artifacts(tenant_id,id) ON DELETE CASCADE
);
ALTER TABLE photo.v29_perspective_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v29_perspective_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v29_perspective_lines TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v29_perspective_lines FROM PUBLIC;
GRANT SELECT,INSERT ON photo.v29_perspective_lines TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_lines_document(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,p_lines uuid,p_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_lines IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
 OR photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,
 'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) INTO result
 FROM photo.v29_perspective_lines l JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
 WHERE l.tenant_id=p_tenant AND l.project_id=p_project AND l.snapshot_id=p_snapshot
 AND l.asset_version_id=(p_base->>'asset_version_id')::uuid
 AND l.source_id=(p_base->'source'->>'artifact_id')::uuid AND l.source_sha=p_base->'source'->>'sha256'
 AND l.canvas_hash=p_base->>'canvas_hash' AND l.artifact_id=p_lines AND l.sha256=p_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF l,a;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_perspective_lines_document(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_document(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_document(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
