-- +goose Up
-- Exact two-output delivery encode evidence; every read repeats current context.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_encode_artifact(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_artifact uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,role text,source text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,
 CASE WHEN u.artifact_id IS NOT NULL THEN u.role ELSE NULL END,
 CASE WHEN u.artifact_id IS NOT NULL THEN 'output' ELSE 'input' END
 FROM photo.v30_delivery_encode_context(p_tenant,p_task) c
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=p_artifact
 LEFT JOIN photo.output_uploads u ON u.tenant_id=p_tenant AND u.artifact_id=a.id
 AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready'
 WHERE c.attempt=p_attempt AND c.fence=p_fence AND c.result_id=p_result
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND ((u.artifact_id IS NOT NULL AND a.created_by_task_id=p_task AND a.produced_attempt=p_attempt
 AND a.produced_fence=p_fence AND a.sha256=u.sha256 AND a.output_role=u.role
 AND EXISTS(SELECT 1 FROM photo.task_results z CROSS JOIN LATERAL jsonb_array_elements(z.outputs) output
 WHERE z.tenant_id=p_tenant AND z.task_id=p_task AND z.id=p_result
 AND output->>'artifact_id'=a.id::text AND output->>'sha256'=a.sha256))
 OR (u.artifact_id IS NULL AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=a.id AND i.sha256=a.sha256)));
$$;
ALTER FUNCTION photo.v30_delivery_encode_artifact(uuid,uuid,integer,bigint,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_encode_artifact(uuid,uuid,integer,bigint,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_encode_artifact(uuid,uuid,integer,bigint,uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.v30_delivery_encode_staged_outputs(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,state text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT u.artifact_id,u.sha256,u.state FROM photo.v30_delivery_encode_context(p_tenant,p_task) c
 JOIN photo.output_uploads u ON u.tenant_id=p_tenant AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence
 WHERE c.attempt=p_attempt AND c.fence=p_fence AND c.result_id=p_result;
$$;
ALTER FUNCTION photo.v30_delivery_encode_staged_outputs(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_encode_staged_outputs(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_encode_staged_outputs(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
