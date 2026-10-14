-- +goose Up
-- Original accepted snapshot source; object-byte and ICC verification remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_worker_negative_source_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;body jsonb;auth_session uuid;
BEGIN
 IF photo.v29_negative_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 body:=p_json::jsonb;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 auth_session:=(r.config_snapshot->'v29_negative'->>'auth_session_id')::uuid;
 IF w.id IS NULL OR w.workflow_type IS DISTINCT FROM 'preview'
 OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM w.project_id::text
 OR photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,auth_session,body->'base') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.asset_versions v
 ON v.tenant_id=i.tenant_id AND v.id=(body->'base'->>'asset_version_id')::uuid
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts original ON original.tenant_id=a.tenant_id AND original.id=a.original_artifact_id
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='WorkingImage'
 AND i.artifact_id::text=body->'base'->'source'->>'artifact_id'
 AND i.sha256=body->'base'->'source'->>'sha256'
 AND original.state='ready' AND original.verified_at IS NOT NULL AND original.bytes>0
 AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null')
 AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 RETURN photo.v29_negative_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_worker_negative_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_worker_negative_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_worker_negative_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
