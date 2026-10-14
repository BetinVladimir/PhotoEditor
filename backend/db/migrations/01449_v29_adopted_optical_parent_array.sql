-- +goose Up
-- Exact child occurrence under the adopted parent document/result.
-- Original numeric verification and replacement membership remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_adopted_optical_parent_array(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_task uuid,p_run uuid,p_result uuid,p_source text,p_child uuid,p_sha text,p_role text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected jsonb;image jsonb;ref jsonb;step integer;item photo.artifacts%ROWTYPE;
BEGIN
 selected:=photo.read_v29_adopted_optical_parent(p_tenant,p_project,p_actor,p_session,p_task,p_run);
 IF selected IS NULL OR p_result IS NULL OR selected->>'result_id' IS DISTINCT FROM p_result::text OR p_source IS NULL OR octet_length(p_source) NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(p_source,'UTF8')),'hex') IS DISTINCT FROM selected->'source'->>'sha256'
 OR p_child IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 image:=p_source::jsonb;
 IF p_role='WorkingPixels' THEN ref:=image->'pixels';
 ELSIF p_role='WorkingAlpha' THEN ref:=image->'alpha';
 ELSIF p_role ~ '^InverseField/(0|[1-9][0-9]{0,2})$' THEN
 step:=split_part(p_role,'/',2)::integer;
 IF step>=128 OR image->'transform_chain'->'steps'->step->>'kind' IS DISTINCT FROM 'dense_warp' THEN RETURN NULL;END IF;
 ref:=image->'transform_chain'->'steps'->step->'inverse_field';
 ELSE RETURN NULL;END IF;
 IF ref IS NULL OR ref->>'artifact_id' IS DISTINCT FROM p_child::text OR ref->>'sha256' IS DISTINCT FROM p_sha THEN RETURN NULL;END IF;
 SELECT * INTO item FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_child AND sha256=p_sha FOR SHARE;
 IF NOT FOUND OR item.state IS DISTINCT FROM 'ready' OR item.verified_at IS NULL OR item.bytes<=0
 OR (item.media_type IS DISTINCT FROM 'application/x-npy' AND NOT(p_role IN('WorkingPixels','WorkingAlpha') AND coalesce(item.media_type,'')='application/octet-stream')) OR item.object_version IS NULL OR item.object_version IN('','null')
 OR (item.expires_at IS NOT NULL AND item.expires_at<=clock_timestamp()) THEN RETURN NULL;END IF;
 IF p_role='WorkingPixels' AND coalesce(item.output_role,'') NOT IN('working_rgb','pixels') THEN RETURN NULL;END IF;
 IF p_role='WorkingAlpha' AND item.output_role IS DISTINCT FROM 'alpha' THEN RETURN NULL;END IF;
 IF p_role LIKE 'InverseField/%' AND item.output_role IS DISTINCT FROM 'warp_field' THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',item.id,'sha256',item.sha256,'bucket',item.bucket,'object_key',item.object_key,'object_version',item.object_version,'bytes',item.bytes,'media_type',item.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_adopted_optical_parent_array(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_adopted_optical_parent_array(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_adopted_optical_parent_array(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
