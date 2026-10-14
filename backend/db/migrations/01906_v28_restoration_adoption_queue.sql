-- +goose Up
-- Discovery metadata only. Every candidate is reauthorized and physically
-- verified before the dedicated receipt commit.
-- +goose StatementBegin
CREATE FUNCTION photo.next_v28_restoration_adoption(p_after uuid DEFAULT NULL)
RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid,project_id uuid,
 snapshot_id uuid,actor_id uuid,session_id uuid,operation text,
 descriptor_jcs text,result_id uuid,root_id uuid,root_sha text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.run_id,t.id,w.project_id,r.input_snapshot_id,
  r.created_by,r.v28_restoration_session_id,t.operation,
  r.v28_restoration_descriptor_jcs,z.id,root.id,root.sha256
 FROM photo.tasks t
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
  AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
  AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(z.outputs)='array'
  THEN z.outputs ELSE '[]'::jsonb END) AS output(value) ON true
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id
  AND root.id::text=output.value->>'artifact_id'
  AND root.sha256=output.value->>'sha256'
 WHERE (p_after IS NULL OR t.id>p_after)
  AND t.operation IN('portrait.glare','image.reflection')
  AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
  AND t.canonical_result_id IS NOT NULL AND t.attempt>0 AND t.fencing_token>0
  AND z.review_required AND jsonb_typeof(z.outputs)='array'
  AND jsonb_array_length(z.outputs)=1
  AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
  AND r.status IN('running','waiting_review')
  AND r.v28_restoration_descriptor_jcs IS NOT NULL
  AND r.v28_restoration_session_id IS NOT NULL
  AND r.v28_restoration_adoption_receipt IS NULL
  AND r.input_snapshot_id IS NOT NULL
  AND (r.v28_restoration_descriptor_jcs::jsonb)->'task'->>'id'=t.id::text
  AND (r.v28_restoration_descriptor_jcs::jsonb)->'task'->>'operation'=t.operation
  AND root.output_role='document' AND root.media_type='application/json'
  AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt
  AND root.produced_fence=t.fencing_token AND root.state='ready'
  AND root.verified_at IS NOT NULL AND root.object_version IS NOT NULL
  AND root.object_version NOT IN('','null')
  AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
   AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 ORDER BY t.id LIMIT 1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.next_v28_restoration_adoption(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v28_restoration_adoption(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.next_v28_restoration_adoption(uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
