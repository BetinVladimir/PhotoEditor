-- +goose Up
-- Align original WorkingImage source bounds with the detector1MiB contract.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_v29_perspective_source_child(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,
 p_base jsonb,p_source text,p_child uuid,p_sha text,p_role text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE image jsonb;ref jsonb;step integer;item photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_source IS NULL OR octet_length(p_source) NOT BETWEEN 2 AND 1048576
 OR encode(sha256(convert_to(p_source,'UTF8')),'hex') IS DISTINCT FROM p_base->'source'->>'sha256'
 OR photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true
 OR p_child IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 image:=p_source::jsonb;
 IF p_role='WorkingPixels' THEN ref:=image->'pixels';
 ELSIF p_role='WorkingAlpha' THEN ref:=image->'alpha';
 ELSIF p_role ~ '^InverseField/(0|[1-9][0-9]{0,2})$' THEN
 step:=split_part(p_role,'/',2)::integer;
 ref:=image->'transform_chain'->'steps'->step->'inverse_field';
 ELSE RETURN false;END IF;
 IF ref IS NULL OR ref->>'artifact_id' IS DISTINCT FROM p_child::text OR ref->>'sha256' IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 SELECT * INTO item FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_child AND sha256=p_sha FOR SHARE;
 IF NOT FOUND OR item.state IS DISTINCT FROM 'ready' OR item.verified_at IS NULL OR item.bytes<=0
 OR (item.media_type IS DISTINCT FROM 'application/x-npy' AND NOT(p_role IN('WorkingPixels','WorkingAlpha') AND coalesce(item.media_type,'')='application/octet-stream')) OR item.object_version IS NULL OR item.object_version IN('','null')
 OR (item.expires_at IS NOT NULL AND item.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
 IF p_role='WorkingPixels' AND coalesce(item.output_role,'') NOT IN('working_rgb','pixels') THEN RETURN false;END IF;
 IF p_role='WorkingAlpha' AND coalesce(item.output_role,'') NOT IN('alpha','working_alpha') THEN RETURN false;END IF;
 IF p_role LIKE 'InverseField/%' AND item.output_role IS DISTINCT FROM 'warp_field' THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_perspective_source_child(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_perspective_source_child(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_perspective_source_child(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v29_perspective_source_document(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;
BEGIN
 IF photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,
 'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) INTO result
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(p_base->'source'->>'artifact_id')::uuid
 AND a.sha256=p_base->'source'->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_perspective_source_document(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_source_document(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_source_document(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
