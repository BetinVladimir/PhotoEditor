-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_manual_preparation_output_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_mask uuid,p_mask_sha text,p_output uuid,p_output_sha text,p_role text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT child.bucket,child.object_key,child.object_version,child.bytes,child.media_type
 FROM photo.artifacts pixel
 JOIN photo.tasks producer ON producer.tenant_id=pixel.tenant_id AND producer.id=pixel.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts child ON child.tenant_id=producer.tenant_id AND child.created_by_task_id=producer.id AND child.id=p_output AND child.sha256=p_output_sha
 WHERE (SELECT count(*) FROM photo.semantic_proposal_manual_preparation_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_mask,p_mask_sha))=1
 AND pixel.tenant_id=p_tenant AND pixel.id=p_mask AND pixel.sha256=p_mask_sha
 AND p_role IN('mask_protected','mask_target_png','mask_protected_png') AND child.output_role=p_role
 AND child.state='ready' AND child.verified_at IS NOT NULL AND child.object_key<>''
 AND child.object_version IS NOT NULL AND child.object_version NOT IN('','null')
 AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp())
 AND ((p_role='mask_protected' AND child.media_type='application/octet-stream' AND child.bytes BETWEEN 32 AND 800065548)
 OR (p_role IN('mask_target_png','mask_protected_png') AND child.media_type='image/png' AND child.bytes BETWEEN 57 AND 613048576))
 AND child.produced_attempt=producer.attempt AND child.produced_fence=producer.fencing_token
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_output,'sha256',p_output_sha));
$$;
ALTER FUNCTION photo.semantic_proposal_manual_preparation_output_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_manual_preparation_output_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_manual_preparation_output_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
