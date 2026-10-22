-- +goose Up
-- Scheduler observation of one exact adopted occurrence within the full batch.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_preview_source(p_tenant uuid,p_task uuid)
RETURNS TABLE(project_id uuid,run_id uuid,workflow_id uuid,result_id uuid,
 root_id uuid,root_sha text,attempt integer,fence bigint,actor_id uuid,auth_session_id uuid,
 source_version_id uuid,source_id uuid,source_sha text,canvas_hash text,ordinal integer)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.project_id,r.id,w.id,b.result_id,b.artifact_id,b.sha256,t.attempt,t.fencing_token,
 r.created_by,(r.config_snapshot->'v25_lens'->>'auth_session_id')::uuid,
 (n.node->'params'->'base'->>'asset_version_id')::uuid,b.source_artifact_id,b.source_sha256,b.canvas_hash,split_part(t.node_key,':',2)::integer
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.projects project ON project.tenant_id=w.tenant_id AND project.id=w.project_id AND project.archived_at IS NULL
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(r.config_snapshot->'v25_lens'->'nodes')='array'
 THEN r.config_snapshot->'v25_lens'->'nodes' ELSE '[]'::jsonb END) n(node)
 CROSS JOIN LATERAL photo.v25_adopted_lens_binding(p_tenant,w.project_id,
 (n.node->'params'->'base'->>'asset_version_id')::uuid,p_task,r.created_by,
 (r.config_snapshot->'v25_lens'->>'auth_session_id')::uuid) b
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND t.tenant_id=p_tenant AND t.id=p_task AND n.node->>'task_id'=t.id::text
 AND r.status IN('running','waiting_review','completed') AND NOT r.cancel_requested AND r.kind='v2' AND r.graph_sealed
 AND r.config_snapshot->>'project_id'=w.project_id::text
 AND r.config_snapshot->>'input_snapshot_id'=r.input_snapshot_id::text;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_preview_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_preview_source(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_lens_preview_source(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
