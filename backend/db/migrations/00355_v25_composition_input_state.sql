-- +goose Up
-- Narrow scheduler read for input registration/replay; no table grants.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_composition_input_state(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS TABLE(sealed boolean,manifest jsonb,entries_sha text,entry_count text,chunks jsonb)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v2_input_closure_ready(t.id),t.execution_profile->'v2_input_manifest',
  t.execution_profile->>'v2_entries_sha',t.execution_profile->>'v2_entry_count',
  coalesce((SELECT jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id::text,'sha256',i.sha256) ORDER BY i.ordinal)
   FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.contract='InputClosureChunk'),'[]'::jsonb)
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task
  AND t.operation='layer.compose' AND t.operation_version='1' AND t.resource_class='cpu'
  AND t.state='blocked' AND t.attempt=0 AND NOT t.cancel_requested
  AND r.kind='v2' AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
  AND w.renderer_version='v2.2'
  AND photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,
   (r.config_snapshot->'v25_composition'->>'auth_session_id')::uuid);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_composition_input_state(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_composition_input_state(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_composition_input_state(uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
