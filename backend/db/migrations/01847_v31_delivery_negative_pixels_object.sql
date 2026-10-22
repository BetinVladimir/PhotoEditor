-- +goose Up
-- Exact output RGB object for a delegated optical-negative task.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_negative_pixels_object(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,p_task uuid,
 p_root uuid,p_root_sha text,p_descriptor uuid,p_descriptor_sha text,
 p_pixels uuid,p_pixels_sha text,p_bucket text,p_min_bytes bigint)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;result jsonb;
BEGIN
 IF p_root_sha!~'^[0-9a-f]{64}$' OR p_descriptor_sha!~'^[0-9a-f]{64}$'
  OR p_pixels_sha!~'^[0-9a-f]{64}$' OR p_min_bytes<12 OR
  length(p_bucket)<3 OR length(p_bucket)>63 THEN RETURN NULL;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN NULL;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'optical.negative' OR e->>'version'<>'1'
  OR e->'outputs'<>jsonb_build_array(jsonb_build_object(
   'artifact_id',p_root::text,'sha256',p_root_sha)) THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('bucket',a.bucket,'key',a.object_key,
  'version',a.object_version,'sha256',a.sha256,'bytes',a.bytes,
  'media_type',a.media_type)
 INTO result FROM photo.artifacts a
 JOIN photo.v2_workflows w ON w.tenant_id=a.tenant_id
  AND w.run_id=(e->>'run')::uuid AND w.project_id=(source->>'project_id')::uuid
 JOIN photo.v2_artifact_links pixels_link ON pixels_link.tenant_id=w.tenant_id
  AND pixels_link.workflow_id=w.id AND pixels_link.project_id=w.project_id
  AND pixels_link.artifact_id=a.id AND pixels_link.sha256=a.sha256
 JOIN photo.v2_artifact_links descriptor_link ON descriptor_link.tenant_id=w.tenant_id
  AND descriptor_link.workflow_id=w.id AND descriptor_link.project_id=w.project_id
  AND descriptor_link.artifact_id=p_descriptor AND descriptor_link.sha256=p_descriptor_sha
 JOIN photo.v2_artifact_links root_link ON root_link.tenant_id=w.tenant_id
  AND root_link.workflow_id=w.id AND root_link.project_id=w.project_id
  AND root_link.artifact_id=p_root AND root_link.sha256=p_root_sha
 JOIN photo.artifacts root_artifact ON root_artifact.tenant_id=root_link.tenant_id
  AND root_artifact.id=root_link.artifact_id AND root_artifact.sha256=root_link.sha256
 WHERE a.tenant_id=(source->>'tenant_id')::uuid
  AND w.workflow_type='preview' AND w.renderer_version='v2.2'
  AND a.id=p_pixels AND a.sha256=p_pixels_sha AND a.bucket=p_bucket
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null') AND a.media_type='application/x-npy'
  AND a.output_role='pixels' AND a.bytes BETWEEN p_min_bytes AND p_min_bytes+65536
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.created_by_task_id=p_task AND a.produced_attempt=(e->>'attempt')::integer
  AND a.produced_fence=(e->>'fence')::bigint
  AND pixels_link.purpose=encode(sha256(convert_to(
   'tasks/'||p_task||'/$/working_image/pixels','UTF8')),'hex')
  AND descriptor_link.purpose=encode(sha256(convert_to(
   'tasks/'||p_task||'/$/working_image','UTF8')),'hex')
  AND root_link.ordinal=0 AND root_link.purpose=encode(sha256(convert_to(
   'tasks/'||p_task||'/$','UTF8')),'hex')
  AND root_artifact.state='ready' AND root_artifact.verified_at IS NOT NULL
  AND root_artifact.object_version NOT IN('','null')
  AND root_artifact.created_by_task_id=p_task
  AND root_artifact.produced_attempt=(e->>'attempt')::integer
  AND root_artifact.produced_fence=(e->>'fence')::bigint
  AND root_artifact.output_role='document' AND root_artifact.media_type='application/json'
 FOR SHARE OF a,root_artifact;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_negative_pixels_object(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,uuid,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_negative_pixels_object(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,uuid,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_negative_pixels_object(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,uuid,text,text,bigint)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
