-- +goose Up
-- Independent selected parser labels and declared detector; no defect outputs.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_face_parse_preparation_pixels(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_parse uuid,p_parse_sha text,p_detection uuid,p_detection_sha text,p_pixels uuid,p_pixels_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT pixels.bucket,pixels.object_key,pixels.object_version,pixels.bytes
 FROM photo.artifacts manifest
 JOIN photo.tasks parser ON parser.tenant_id=manifest.tenant_id AND parser.id=manifest.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=parser.tenant_id AND result.id=parser.canonical_result_id AND result.task_id=parser.id AND result.attempt=parser.attempt AND result.fencing_token=parser.fencing_token
 JOIN photo.artifacts pixels ON pixels.tenant_id=parser.tenant_id AND pixels.created_by_task_id=parser.id
 WHERE (SELECT count(*) FROM photo.semantic_proposal_face_preparation_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_parse,p_parse_sha,'face.parse'))=1
 AND (SELECT count(*) FROM photo.semantic_proposal_face_preparation_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_detection,p_detection_sha,'face.detect'))=1
 AND manifest.tenant_id=p_tenant AND manifest.id=p_parse AND manifest.sha256=p_parse_sha
 AND parser.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_detection,'sha256',p_detection_sha))
 AND pixels.id=p_pixels AND pixels.sha256=p_pixels_sha AND pixels.output_role='face_labels'
 AND pixels.id NOT IN(p_source,p_parse,p_detection,parser.params_artifact_id)
 AND pixels.produced_attempt=parser.attempt AND pixels.produced_fence=parser.fencing_token
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',pixels.id,'sha256',pixels.sha256))
 AND pixels.media_type='application/octet-stream' AND pixels.bytes BETWEEN 32 AND 400065548
 AND pixels.state='ready' AND pixels.verified_at IS NOT NULL AND pixels.object_key<>''
 AND pixels.object_version IS NOT NULL AND pixels.object_version NOT IN('','null')
 AND (pixels.expires_at IS NULL OR pixels.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_proposal_face_parse_preparation_pixels(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_face_parse_preparation_pixels(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_face_parse_preparation_pixels(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
