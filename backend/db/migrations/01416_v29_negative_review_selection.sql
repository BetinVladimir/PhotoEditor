-- +goose Up
-- Read immutable human selection without rewriting produced task outputs.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_review_selection(p_tenant uuid,p_task uuid)
RETURNS TABLE(review_id uuid,decision_hash text,route text,image_id uuid,image_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT v.id,v.decision_hash,v.decision_route,
 CASE v.decision_route WHEN 'candidate' THEN c.id WHEN 'base' THEN b.id ELSE NULL END,
 CASE v.decision_route WHEN 'candidate' THEN c.sha256 WHEN 'base' THEN b.sha256 ELSE NULL END
 FROM photo.reviews v JOIN photo.tasks t ON t.tenant_id=v.tenant_id AND t.id=v.task_id AND t.run_id=v.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.artifacts c ON c.tenant_id=v.tenant_id AND c.id=v.candidate_artifact_id AND c.sha256=v.candidate_hash
 JOIN photo.artifacts b ON b.tenant_id=v.tenant_id AND b.id=v.base_artifact_id
 JOIN photo.v2_artifact_links root_link ON root_link.tenant_id=w.tenant_id AND root_link.workflow_id=w.id
 AND root_link.project_id=w.project_id AND root_link.ordinal=0
 AND root_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND root.id=root_link.artifact_id AND root.sha256=root_link.sha256
 WHERE v.tenant_id=p_tenant AND v.task_id=p_task AND v.kind='optical.negative'
 AND t.operation=v.kind AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
 AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
 AND v.state='decided' AND v.revision=2 AND v.decided_by IS NOT NULL AND v.decided_at IS NOT NULL
 AND v.decision_hash ~ '^[0-9a-f]{64}$'
 AND ((v.decision='accept' AND v.decision_route='candidate') OR (v.decision='reject' AND v.decision_route='base')
 OR (v.decision='skip' AND v.decision_route='skip'))
 AND v.selection=jsonb_build_object('version','candidate_review_v1','candidate',jsonb_build_object('artifact_id',c.id::text,'sha256',c.sha256),
 'base',jsonb_build_object('artifact_id',b.id::text,'sha256',b.sha256),'revision',2)
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token AND z.review_required
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id::text,'sha256',root.sha256))
 AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
 AND c.created_by_task_id=t.id AND c.produced_attempt=t.attempt AND c.produced_fence=t.fencing_token
 AND root.state='ready' AND c.state='ready' AND b.state='ready'
 AND root.verified_at IS NOT NULL AND c.verified_at IS NOT NULL AND b.verified_at IS NOT NULL
 AND root.object_version NOT IN('','null') AND c.object_version NOT IN('','null') AND b.object_version NOT IN('','null')
 AND root.media_type='application/json' AND c.media_type='application/json' AND b.media_type='application/json'
 AND root.output_role='document' AND c.output_role='working_image'
 AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND (b.expires_at IS NULL OR b.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.project_id=w.project_id
 AND i.artifact_id=b.id AND i.sha256=b.sha256 AND i.contract='WorkingImage')
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links child WHERE child.tenant_id=w.tenant_id AND child.workflow_id=w.id
 AND child.project_id=w.project_id AND child.artifact_id=c.id AND child.sha256=c.sha256
 AND child.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex'))
 AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 AND NOT EXISTS(SELECT 1 FROM photo.reviews other WHERE other.tenant_id=t.tenant_id AND other.task_id=t.id AND other.id<>v.id)
$$;
ALTER FUNCTION photo.v29_negative_review_selection(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_review_selection(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_negative_review_selection(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
