-- +goose Up
-- Accepted original source authority only; depth/mask producers remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_worker_relight_source_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope record; base jsonb;
BEGIN
 IF photo.v28_relight_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 base:=p_body::jsonb->'base';
 SELECT w.project_id,w.input_snapshot_id,r.created_by,
 (r.config_snapshot->'v28_relight'->>'auth_session_id')::uuid AS session_id INTO scope
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run;
 IF scope.project_id IS NULL OR scope.session_id IS NULL THEN RETURN false;END IF;
 PERFORM v.id FROM photo.asset_versions v
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts s ON s.tenant_id=v.tenant_id AND s.id=v.master_artifact_id
 WHERE v.tenant_id=p_tenant AND v.id=(base->>'asset_version_id')::uuid
 FOR SHARE OF v,a,s;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=scope.input_snapshot_id FOR SHARE;
 PERFORM id FROM photo.sessions WHERE id=scope.session_id FOR SHARE;
 IF photo.v28_local_portrait_source_scope(p_tenant,scope.project_id,scope.input_snapshot_id,
  scope.created_by,scope.session_id,base) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND i.contract='WorkingImage' AND i.artifact_id::text=base->'source'->>'artifact_id'
  AND i.sha256=base->'source'->>'sha256') THEN RETURN false;END IF;
 RETURN photo.v28_relight_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_worker_relight_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_worker_relight_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_worker_relight_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
