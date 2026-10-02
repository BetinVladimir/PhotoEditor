-- Canonical input/output views for arbitrary immutable Apply v4 stages.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v4_inputs(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS TABLE(ordinal bigint,item_id uuid,asset_id uuid,asset_version_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text,payload jsonb,config_snapshot jsonb,style_artifact_id uuid,style_sha256 text,style_bytes bigint)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.item_id,v.asset_id,i.asset_version_id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,i.payload,r.config_snapshot,sa.id,sa.sha256,sa.bytes
 FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 LEFT JOIN photo.style_versions sv ON sv.tenant_id=b.tenant_id AND sv.id=b.style_version_id AND sv.approval_status='accepted'
 LEFT JOIN photo.artifacts sa ON sa.tenant_id=sv.tenant_id AND sa.id=sv.export_artifact_id AND sa.sha256=sv.config_hash AND sa.state='ready' AND sa.object_version IS NOT NULL
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.graph_version=4 AND b.lease_token=p_token AND b.lease_until>=clock_timestamp()
  AND (b.style_version_id IS NULL OR sa.id IS NOT NULL) ORDER BY i.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_v4_inputs(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v4_inputs(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.apply_v4_inputs(uuid,uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.apply_v4_stage_outputs(p_tenant uuid,p_run uuid,p_token uuid,p_operation text,p_role text)
RETURNS TABLE(ordinal int,item_id uuid,task_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT n.ordinal,n.source_item_id,t.id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.apply_builds b
 JOIN photo.apply_v4_stages current_stage ON current_stage.tenant_id=b.tenant_id AND current_stage.run_id=b.run_id AND current_stage.stage_index=b.stage_index
 JOIN photo.apply_v4_stages requested_stage ON requested_stage.tenant_id=b.tenant_id AND requested_stage.run_id=b.run_id AND requested_stage.operation=p_operation AND requested_stage.output_role=p_role
  AND (requested_stage.stage_index<current_stage.stage_index OR b.phase='adopt' AND requested_stage.stage_index=current_stage.stage_index)
 JOIN photo.apply_nodes n ON n.tenant_id=requested_stage.tenant_id AND n.run_id=requested_stage.run_id AND n.phase=requested_stage.phase AND n.operation=requested_stage.operation
 JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
 JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id AND tr.task_id=t.id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=tr.attempt AND a.produced_fence=tr.fencing_token
  AND a.output_role=p_role AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.graph_version=4 AND b.lease_token=p_token AND b.lease_until>=clock_timestamp()
  AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) ORDER BY n.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_v4_stage_outputs(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v4_stage_outputs(uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.apply_v4_stage_outputs(uuid,uuid,uuid,text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
