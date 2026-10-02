-- +goose Up
-- Exact mask document occurrence in an adopted lens result. Other mask
-- producers and user masks need their own lineage proof; no project fallback.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_lens_mask_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_actor uuid,p_session uuid,p_result uuid,p_result_sha text,p_source uuid,p_source_sha text,p_canvas text,
 p_mask uuid,p_mask_sha text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v25_layer_lens_proposal_scope(p_tenant,p_project,p_version,p_actor,p_session,
  p_result,p_result_sha,p_source,p_source_sha,p_canvas)
 AND EXISTS(SELECT 1 FROM photo.artifacts root
  JOIN photo.tasks t ON t.tenant_id=root.tenant_id AND t.id=root.created_by_task_id
  JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id AND w.project_id=p_project
  JOIN photo.v2_artifact_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id AND l.project_id=p_project
   AND l.artifact_id=p_mask AND l.sha256=p_mask_sha
  JOIN photo.artifacts m ON m.tenant_id=l.tenant_id AND m.id=l.artifact_id AND m.sha256=l.sha256
  WHERE root.tenant_id=p_tenant AND root.id=p_result AND root.sha256=p_result_sha
   AND m.created_by_task_id=t.id AND m.produced_attempt=t.attempt AND m.produced_fence=t.fencing_token
   AND m.output_role='mask' AND m.media_type='application/json' AND m.bytes BETWEEN 2 AND 8388608
   AND m.state='ready' AND m.verified_at IS NOT NULL AND m.object_version IS NOT NULL
   AND m.object_version NOT IN('','null') AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp())
   AND l.purpose IN(
    encode(sha256(convert_to('tasks/'||t.id::text||'/$/changed_mask','UTF8')),'hex'),
    encode(sha256(convert_to('tasks/'||t.id::text||'/$/generated_mask','UTF8')),'hex')));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_lens_mask_scope(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_lens_mask_scope(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_lens_mask_scope(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
