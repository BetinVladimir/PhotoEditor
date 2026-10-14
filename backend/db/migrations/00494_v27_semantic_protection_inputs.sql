-- +goose Up
-- Frozen compiler nodes: task_id, params_artifact_id, params_sha, source_protection, target_protection.
-- This binds refs, not mask document pixels or semantic/model qualification.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_adoption_protection(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(source_artifact_id uuid,source_sha text,target_artifact_id uuid,target_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH admitted AS (
  SELECT artifact_id,sha256 FROM photo.v27_semantic_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result)
 ), nodes AS (
  SELECT node FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
  JOIN admitted a ON a.artifact_id=t.params_artifact_id
  CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(r.config_snapshot->'v27_semantic'->'nodes')='array'
   THEN CASE WHEN jsonb_array_length(r.config_snapshot->'v27_semantic'->'nodes') BETWEEN 1 AND 5000
    THEN r.config_snapshot->'v27_semantic'->'nodes' ELSE '[]'::jsonb END ELSE '[]'::jsonb END) node
  WHERE t.tenant_id=p_tenant AND t.id=p_task AND node->>'task_id'=p_task::text AND node->>'params_sha'=a.sha256
   AND node->>'params_artifact_id'=a.artifact_id::text
 ), valid AS (
  SELECT node FROM nodes WHERE (SELECT count(*) FROM nodes)=1
   AND jsonb_typeof(node->'source_protection')='object' AND jsonb_typeof(node->'target_protection')='object'
   AND ((node->'source_protection')-'artifact_id'-'sha256')='{}'::jsonb
   AND ((node->'target_protection')-'artifact_id'-'sha256')='{}'::jsonb
 )
 SELECT src.id,src.sha256,dst.id,dst.sha256 FROM valid n
 JOIN photo.artifacts src ON src.tenant_id=p_tenant AND src.id::text=n.node->'source_protection'->>'artifact_id'
  AND src.sha256=n.node->'source_protection'->>'sha256'
 JOIN photo.artifacts dst ON dst.tenant_id=p_tenant AND dst.id::text=n.node->'target_protection'->>'artifact_id'
  AND dst.sha256=n.node->'target_protection'->>'sha256'
 WHERE src.state='ready' AND dst.state='ready' AND src.media_type='application/json' AND dst.media_type='application/json'
  AND src.verified_at IS NOT NULL AND dst.verified_at IS NOT NULL
  AND src.object_version IS NOT NULL AND src.object_version NOT IN('','null')
  AND dst.object_version IS NOT NULL AND dst.object_version NOT IN('','null')
  AND (src.expires_at IS NULL OR src.expires_at>clock_timestamp()) AND (dst.expires_at IS NULL OR dst.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.artifact_id=src.id AND i.sha256=src.sha256 AND i.contract='MaskRef')
  AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.artifact_id=dst.id AND i.sha256=dst.sha256 AND i.contract='MaskRef');
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v27_semantic_adoption_protection(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_adoption_protection(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_adoption_protection(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
