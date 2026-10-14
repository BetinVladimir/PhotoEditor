-- +goose Up
-- Dedicated replacement result observation; caller also holds physical source locks.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_replacement_detector_lines(p_tenant uuid,p_task uuid,p_proof jsonb)
RETURNS TABLE(result_id uuid,artifact_id uuid,sha256 text,result_jcs text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d jsonb;
BEGIN
 IF p_proof IS NULL OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object' THEN RETURN;END IF;
 d:=(p_proof->>'descriptor_jcs')::jsonb;
 IF d->>'tenant_id' IS DISTINCT FROM p_tenant::text OR d->>'task_id' IS DISTINCT FROM p_task::text
 OR photo.lock_v29_replacement_detector_preparation(p_tenant,(p_proof->>'project_id')::uuid,
 (p_proof->>'actor_id')::uuid,(p_proof->>'auth_session_id')::uuid,(p_proof->>'review_id')::uuid,
 p_proof->>'decision_hash',p_proof->'plan',p_proof->>'descriptor_jcs',p_proof->>'original_jcs',
 (p_proof->>'snapshot_id')::uuid) IS DISTINCT FROM true
 OR photo.v29_replacement_detector_adoption_committed(p_tenant,p_task,p_proof->>'descriptor_jcs',
 (p_proof->>'snapshot_id')::uuid) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY
 SELECT z.id,a.id,a.sha256,r.v29_replacement_detector_adoption_receipt->>'result_jcs'
 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id AND w.project_id=(d->>'project_id')::uuid
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id::text=z.outputs->0->>'artifact_id' AND a.sha256=z.outputs->0->>'sha256'
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND r.id=(d->>'run_id')::uuid
 AND w.renderer_version='v2.2' AND w.workflow_type='preview'
 AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 AND a.output_role='document' AND a.media_type='application/json' AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND octet_length(r.v29_replacement_detector_adoption_receipt->>'result_jcs') BETWEEN 2 AND 1048576
 AND encode(sha256(convert_to(r.v29_replacement_detector_adoption_receipt->>'result_jcs','UTF8')),'hex')=a.sha256
 AND (r.v29_replacement_detector_adoption_receipt->>'result_jcs')::jsonb->'base'=d->'base'
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=w.tenant_id AND l.workflow_id=w.id
 AND l.project_id=w.project_id AND l.artifact_id=a.id AND l.sha256=a.sha256)
 FOR SHARE OF t,r,w,z,a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.read_v29_replacement_detector_lines(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_replacement_detector_lines(uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_replacement_detector_lines(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
