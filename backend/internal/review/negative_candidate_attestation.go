package review

// Negative publishes a single ImageResult root rather than a direct WorkingImage
// output. Candidate approval must follow the exact adopted child edge; adding
// a ready artifact to the same task does not make it a candidate.
const negativeCandidateAttestationSQL = `SELECT EXISTS(
 SELECT 1 FROM photo.tasks t
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.v2_artifact_links root_link ON root_link.tenant_id=w.tenant_id AND root_link.workflow_id=w.id AND root_link.project_id=w.project_id
 JOIN photo.artifacts root ON root.tenant_id=root_link.tenant_id AND root.id=root_link.artifact_id AND root.sha256=root_link.sha256
 JOIN photo.v2_artifact_links child_link ON child_link.tenant_id=w.tenant_id AND child_link.workflow_id=w.id AND child_link.project_id=w.project_id
 JOIN photo.artifacts c ON c.tenant_id=child_link.tenant_id AND c.id=child_link.artifact_id AND c.sha256=child_link.sha256
 JOIN photo.artifacts b ON b.tenant_id=t.tenant_id AND b.id=$6
 WHERE t.tenant_id=$1 AND t.id=$2 AND t.run_id=$3 AND t.operation='optical.negative' AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
 AND root_link.ordinal=0 AND root_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
 AND child_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id::text,'sha256',root.sha256))
 AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
 AND c.id=$4 AND c.sha256=$5 AND c.created_by_task_id=t.id AND c.produced_attempt=t.attempt AND c.produced_fence=t.fencing_token
 AND root.state='ready' AND root.verified_at IS NOT NULL AND root.media_type='application/json' AND root.output_role='document'
 AND c.state='ready' AND c.verified_at IS NOT NULL AND c.media_type='application/json' AND c.output_role='working_image'
 AND b.state='ready' AND b.verified_at IS NOT NULL AND b.media_type='application/json'
 AND coalesce(root.object_version,'') NOT IN('','null') AND coalesce(c.object_version,'') NOT IN('','null') AND coalesce(b.object_version,'') NOT IN('','null')
 AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()) AND (b.expires_at IS NULL OR b.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.artifact_id=b.id AND i.sha256=b.sha256 AND i.contract='WorkingImage')
 AND EXISTS(SELECT 1 FROM photo.artifacts params
 JOIN photo.v2_task_inputs pi ON pi.tenant_id=t.tenant_id AND pi.task_id=t.id AND pi.project_id=w.project_id
 AND pi.artifact_id=params.id AND pi.sha256=params.sha256 AND pi.contract='Params'
 WHERE params.tenant_id=t.tenant_id AND params.id=t.params_artifact_id AND params.id=$7 AND params.sha256=$8
 AND params.state='ready' AND params.verified_at IS NOT NULL AND params.media_type='application/json'
 AND params.bytes BETWEEN 2 AND 65536 AND params.object_version NOT IN('','null')
 AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp()))
)`
