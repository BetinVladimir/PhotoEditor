-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_assembly_worker_snapshot(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_digest text)
RETURNS TABLE(snapshot_artifact_id uuid,snapshot_sha text,snapshot_id uuid,snapshot_digest text,ordinal bigint,asset_version_id uuid,source_id uuid,source_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; pin jsonb; auth_session uuid;
BEGIN
 IF photo.v27_semantic_assembly_worker_qualification(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_digest) IS NULL THEN RETURN;END IF;
 SELECT runs.* INTO r FROM photo.runs runs JOIN photo.tasks t ON t.tenant_id=runs.tenant_id AND t.run_id=runs.id WHERE t.tenant_id=p_tenant AND t.id=p_task;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id;
 pin:=r.config_snapshot->'v27_semantic'->'snapshot';
 IF jsonb_typeof(pin) IS DISTINCT FROM 'object' OR (pin-'artifact_id'-'sha256')<>'{}'::jsonb
 OR coalesce(pin->>'sha256','') !~ '^[0-9a-f]{64}$'
 OR coalesce(r.config_snapshot->'v27_semantic'->>'snapshot_digest','') !~ '^[0-9a-f]{64}$'
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM w.project_id::text
 OR r.config_snapshot->>'input_snapshot_id' IS DISTINCT FROM r.input_snapshot_id::text
 OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id THEN RETURN;END IF;
 auth_session:=(r.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid;
 IF NOT photo.v25_lens_snapshot_ready(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,auth_session) THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256,r.input_snapshot_id,r.config_snapshot->'v27_semantic'->>'snapshot_digest',i.ordinal,i.asset_version_id,i.artifact_id,i.sha256
 FROM photo.artifacts a JOIN photo.v2_task_inputs declared ON declared.tenant_id=a.tenant_id AND declared.artifact_id=a.id AND declared.sha256=a.sha256
 JOIN photo.snapshot_items i ON i.tenant_id=a.tenant_id AND i.snapshot_id=r.input_snapshot_id
 WHERE a.tenant_id=p_tenant AND a.id=(pin->>'artifact_id')::uuid AND a.sha256=pin->>'sha256'
 AND declared.task_id=p_task AND declared.project_id=w.project_id AND declared.contract='InputSnapshot'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) ORDER BY i.ordinal;
EXCEPTION WHEN invalid_text_representation THEN RETURN;
END $$;
ALTER FUNCTION photo.v27_semantic_assembly_worker_snapshot(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_assembly_worker_snapshot(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_assembly_worker_snapshot(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
