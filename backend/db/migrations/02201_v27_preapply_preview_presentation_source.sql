-- +goose Up
-- Authenticated metadata for the single canonical presentation output.
-- Raw presentation still requires exact byte/hash/schema checks after object IO.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_presentation_source(p_tenant uuid,p_preview uuid,
 p_actor uuid,p_session uuid,p_body text,p_sha text)
RETURNS TABLE(task_id uuid,result_id uuid,attempt integer,fence bigint,artifact_id uuid,
 sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;owner uuid;
BEGIN
 PERFORM * FROM photo.inspect_semantic_preapply_preview(p_tenant,p_preview,p_actor,p_session,p_body,p_sha);
 e:=p_body::jsonb;
 owner:=photo.semantic_uuid_v5(p_preview,'semantic.preview_local_exposure:'||(e->>'target_id')::uuid::text);
 RETURN QUERY SELECT t.id,z.id,t.attempt,t.fencing_token,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id=owner
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid
 AND a.sha256=z.outputs->0->>'sha256'
 WHERE r.tenant_id=p_tenant AND r.id=p_preview AND r.kind='preview'
 AND r.status IN('running','completed') AND r.graph_sealed AND NOT r.cancel_requested
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.operation='semantic.preview_local_exposure'
 AND t.operation_version='1' AND t.resource_class='cpu'
 AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND a.output_role='preview_presentation' AND a.media_type='application/json' AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.semantic_preapply_preview_closure_ready(t.id) IS TRUE;
END $$;
ALTER FUNCTION photo.semantic_preapply_preview_presentation_source(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_presentation_source(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_preapply_preview_presentation_source(uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
