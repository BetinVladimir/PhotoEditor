-- +goose Up
-- Read-phase gate. Final adoption must repeat under its transaction locks.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_exposure_adoption_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_params uuid,p_sha text,p_body text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 OR p_sha IS NULL
 OR p_sha !~ '^[0-9a-f]{64}$' OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_sha THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v27_exposure_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result) a
 WHERE a.artifact_id=p_params AND a.sha256=p_sha) THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 project:=(r.config_snapshot->>'project_id')::uuid;e:=p_body::jsonb;
 IF e->'schema_version' IS DISTINCT FROM '1'::jsonb OR e->>'kind' IS DISTINCT FROM 'local_exposure'
 OR e->'preserve_persons' IS DISTINCT FROM 'true'::jsonb
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND bytes=octet_length(p_body))
 OR (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'approved_semantic'->'nodes') n
 WHERE n->>'task_id'=p_task::text AND n->>'params_artifact_id'=p_params::text AND n->>'params_sha'=p_sha
 AND n->>'target_id'=e->>'target_id')<>1 THEN RETURN false;END IF;
 IF NOT photo.semantic_approved_child_edit(p_tenant,t.run_id,(e->>'target_id')::uuid,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,e->'candidate'->>'sha256',
 (e->'source'->'source'->>'artifact_id')::uuid,e->'source'->'source'->>'sha256',
 (e->'intent'->>'artifact_id')::uuid,e->'intent'->>'sha256') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
 AND a.id=(e->'mask'->>'artifact_id')::uuid AND a.sha256=e->'mask'->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
 AND a.id=(e->'transform'->>'artifact_id')::uuid AND a.sha256=e->'transform'->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 RETURN photo.v27_downstream_profile_ready(p_tenant,project,t.operation,t.operation_version,t.resource_class,t.execution_profile)
 AND EXISTS(SELECT 1 FROM photo.v27_exposure_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result) a
 WHERE a.artifact_id=p_params AND a.sha256=p_sha);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v27_exposure_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_exposure_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_exposure_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
