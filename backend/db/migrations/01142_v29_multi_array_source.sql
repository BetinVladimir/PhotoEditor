-- +goose Up
-- Resolve one ordered source child before object IO; publication re-locks scope.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_array_source(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,
 p_operation text,p_params text,p_ordinal integer,p_source text,p_child uuid,p_sha text,p_role text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;binding jsonb;count_frames integer;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN ('optical.panorama','optical.focus_stack')
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
 OR p_ordinal IS NULL OR p_ordinal<0 THEN RETURN NULL;END IF;
 body:=p_params::jsonb;
 IF jsonb_typeof(body->'frames') IS DISTINCT FROM 'array' OR body->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN NULL;END IF;
 count_frames:=jsonb_array_length(body->'frames');
 IF count_frames NOT BETWEEN 2 AND (CASE p_operation WHEN 'optical.panorama' THEN 20 ELSE 30 END)
 OR p_ordinal>=count_frames THEN RETURN NULL;END IF;
 binding:=body->'frames'->p_ordinal;
 IF photo.lock_v29_perspective_source_child(p_tenant,p_project,p_snapshot,p_actor,p_session,binding,p_source,p_child,p_sha,p_role) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_child AND sha256=p_sha FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,
 'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_multi_array_source(uuid,uuid,uuid,uuid,uuid,text,text,integer,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_array_source(uuid,uuid,uuid,uuid,uuid,text,text,integer,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_multi_array_source(uuid,uuid,uuid,uuid,uuid,text,text,integer,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
