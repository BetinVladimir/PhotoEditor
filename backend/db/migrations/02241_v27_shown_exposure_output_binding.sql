-- +goose Up
-- Private output proof. Caller supplies original version-pinned JSON bytes;
-- metadata SHA/size and canonical result membership are checked independently.
-- +goose StatementBegin
CREATE FUNCTION photo.shown_exposure_output_bound(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_params uuid,p_sha text,p_body text,p_root uuid,p_root_sha text,p_output_body text,p_shown_body text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;shown jsonb;output jsonb;root_row photo.artifacts%ROWTYPE;
 shown_row photo.artifacts%ROWTYPE;original photo.artifacts%ROWTYPE;copied photo.artifacts%ROWTYPE;
BEGIN
 IF photo.v27_shown_exposure_adoption_ready(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_sha,p_body)
 IS DISTINCT FROM true THEN RETURN false;END IF;
 IF p_output_body IS NULL OR p_shown_body IS NULL OR octet_length(p_output_body) NOT BETWEEN 2 AND 1048576
 OR octet_length(p_shown_body) NOT BETWEEN 2 AND 1048576 OR p_root IS NULL OR p_root_sha IS NULL
 OR p_root_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 e:=p_body::jsonb;shown:=p_shown_body::jsonb;output:=p_output_body::jsonb;
 IF jsonb_typeof(shown) IS DISTINCT FROM 'object' OR jsonb_typeof(output) IS DISTINCT FROM 'object'
 OR shown->'pixels' IS DISTINCT FROM e->'pixels'
 OR (shown-'pixels') IS DISTINCT FROM (output-'pixels')
 OR output->'pixels'->>'sha256' IS DISTINCT FROM shown->'pixels'->>'sha256'
 OR output->'pixels'->>'artifact_id' IS NULL OR jsonb_typeof(output->'pixels') IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(output->'pixels'))<>2
 OR NOT(output->'pixels' ?& ARRAY['artifact_id','sha256'])
 OR p_root='00000000-0000-0000-0000-000000000000'::uuid
 OR (output->'pixels'->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR p_root=(output->'pixels'->>'artifact_id')::uuid
 OR p_root IN(p_params,(e->'working_image'->>'artifact_id')::uuid,(e->'pixels'->>'artifact_id')::uuid)
 OR (output->'pixels'->>'artifact_id')::uuid IN(p_params,(e->'working_image'->>'artifact_id')::uuid,(e->'pixels'->>'artifact_id')::uuid)
 OR p_root::text IS NOT DISTINCT FROM shown->'alpha'->>'artifact_id'
 OR output->'pixels'->>'artifact_id' IS NOT DISTINCT FROM shown->'alpha'->>'artifact_id'
 THEN RETURN false;END IF;
 SELECT * INTO root_row FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_root FOR SHARE;
 SELECT * INTO shown_row FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(e->'working_image'->>'artifact_id')::uuid FOR SHARE;
 SELECT * INTO original FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(e->'pixels'->>'artifact_id')::uuid FOR SHARE;
 SELECT * INTO copied FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(output->'pixels'->>'artifact_id')::uuid FOR SHARE;
 IF root_row.id IS NULL OR shown_row.id IS NULL OR original.id IS NULL OR copied.id IS NULL
 OR root_row.sha256 IS DISTINCT FROM p_root_sha
 OR root_row.sha256 IS DISTINCT FROM encode(public.digest(convert_to(p_output_body,'UTF8'),'sha256'),'hex')
 OR shown_row.sha256 IS DISTINCT FROM e->'working_image'->>'sha256'
 OR shown_row.sha256 IS DISTINCT FROM encode(public.digest(convert_to(p_shown_body,'UTF8'),'sha256'),'hex')
 OR root_row.bytes IS DISTINCT FROM octet_length(p_output_body)::bigint
 OR shown_row.bytes IS DISTINCT FROM octet_length(p_shown_body)::bigint
 OR root_row.media_type IS DISTINCT FROM 'application/json' OR shown_row.media_type IS DISTINCT FROM 'application/json'
 OR copied.media_type IS DISTINCT FROM 'application/x-npy' OR original.media_type IS DISTINCT FROM 'application/x-npy'
 OR original.sha256 IS DISTINCT FROM e->'pixels'->>'sha256' OR copied.sha256 IS DISTINCT FROM original.sha256
 OR copied.bytes IS DISTINCT FROM original.bytes OR original.bytes NOT BETWEEN 1 AND 2400016384
 OR root_row.created_by_task_id IS DISTINCT FROM p_task OR copied.created_by_task_id IS DISTINCT FROM p_task
 OR root_row.produced_attempt IS DISTINCT FROM p_attempt OR copied.produced_attempt IS DISTINCT FROM p_attempt
 OR root_row.produced_fence IS DISTINCT FROM p_fence OR copied.produced_fence IS DISTINCT FROM p_fence
 OR root_row.output_role IS DISTINCT FROM 'working_image' OR copied.output_role IS DISTINCT FROM 'pixels'
 THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
 AND a.id IN(root_row.id,shown_row.id,original.id,copied.id)
 AND (a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.task_id=p_task
 AND z.id=p_result AND z.attempt=p_attempt AND z.fencing_token=p_fence
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha))) THEN RETURN false;END IF;
 RETURN NOT EXISTS(SELECT 1 FROM (VALUES(root_row.id,root_row.sha256,'working_image'),
 (copied.id,copied.sha256,'pixels')) required(id,sha,role)
 WHERE NOT EXISTS(SELECT 1 FROM photo.output_uploads u WHERE u.tenant_id=p_tenant AND u.task_id=p_task
 AND u.attempt=p_attempt AND u.fence=p_fence AND u.artifact_id=required.id
 AND u.sha256=required.sha AND u.role=required.role AND u.state='ready'));
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.shown_exposure_output_bound(uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.shown_exposure_output_bound(uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
