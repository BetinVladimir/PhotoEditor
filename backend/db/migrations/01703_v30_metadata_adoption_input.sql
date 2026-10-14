-- +goose Up
-- Current scheduler authority for physical metadata verification/adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_metadata_adoption_input(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(proof_jcs text,proof_sha text,root_id uuid,root_sha text,report_id uuid,report_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v30_metadata_result_ready(p_tenant,p_task,p_attempt,p_fence,p_result) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT e.v30_metadata_proof_jcs,e.v30_metadata_proof_sha,root.id,root.sha256,report.id,report.sha256
 FROM photo.tasks t JOIN photo.graph_expansions e ON e.tenant_id=t.tenant_id AND e.run_id=t.run_id AND e.kind='v30.delivery.metadata/1' AND e.state='closed'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN LATERAL (SELECT a.id,a.sha256 FROM jsonb_array_elements(z.outputs) output JOIN photo.artifacts a
 ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256' WHERE a.output_role='encoded_manifest') root ON true
 JOIN LATERAL (SELECT a.id,a.sha256 FROM jsonb_array_elements(z.outputs) output JOIN photo.artifacts a
 ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256' WHERE a.output_role='metadata_report') report ON true
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND z.id=p_result AND z.attempt=p_attempt AND z.fencing_token=p_fence;
END $$;
ALTER FUNCTION photo.read_v30_metadata_adoption_input(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_metadata_adoption_input(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_metadata_adoption_input(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
