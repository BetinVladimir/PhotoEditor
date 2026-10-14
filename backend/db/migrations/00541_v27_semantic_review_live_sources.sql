-- +goose Up
-- +goose StatementBegin
ALTER FUNCTION photo.semantic_review_plan_sources(uuid,uuid,uuid,uuid) RENAME TO semantic_review_plan_sources_pre_current;
REVOKE ALL ON FUNCTION photo.semantic_review_plan_sources_pre_current(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api;

CREATE FUNCTION photo.semantic_review_sources_current(p_tenant uuid,p_workflow uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.tasks assembly ON assembly.tenant_id=r.tenant_id AND assembly.run_id=r.id
  AND assembly.operation='v2.semantic.assemble' AND assembly.node_key='v2.semantic.assemble:0'
 JOIN photo.snapshots snapshot ON snapshot.tenant_id=w.tenant_id AND snapshot.id=w.input_snapshot_id
 WHERE w.tenant_id=p_tenant AND w.id=p_workflow AND w.workflow_type='semantic'
 AND snapshot.item_count BETWEEN 1 AND 5000
 AND (SELECT count(*) FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=snapshot.id)=snapshot.item_count
 AND NOT EXISTS(SELECT 1 FROM photo.snapshot_items i
  LEFT JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
  LEFT JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id AND asset.project_id=w.project_id
  LEFT JOIN photo.artifacts source ON source.tenant_id=i.tenant_id AND source.id=i.artifact_id AND source.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.snapshot_id=snapshot.id AND
   (v.id IS NULL OR asset.id IS NULL OR asset.deleted_at IS NOT NULL OR asset.upload_state<>'verified'
    OR source.id IS NULL OR source.state<>'ready' OR source.verified_at IS NULL OR source.object_version IS NULL
    OR source.object_version IN('','null') OR (source.expires_at IS NOT NULL AND source.expires_at<=clock_timestamp())))
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs declared WHERE declared.tenant_id=p_tenant AND declared.task_id=assembly.id AND declared.contract='TransferCandidate')
 AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs declared
  WHERE declared.tenant_id=p_tenant AND declared.task_id=assembly.id AND declared.contract='TransferCandidate'
  AND NOT EXISTS(SELECT 1 FROM photo.artifacts a
   JOIN photo.tasks producer ON producer.tenant_id=a.tenant_id AND producer.id=a.created_by_task_id AND producer.run_id=r.id
   JOIN photo.task_results z ON z.tenant_id=producer.tenant_id AND z.task_id=producer.id AND z.id=producer.canonical_result_id
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   WHERE a.tenant_id=p_tenant AND a.id=declared.artifact_id AND a.sha256=declared.sha256
    AND producer.operation='semantic.transfer' AND producer.operation_version='1' AND producer.state='succeeded' AND NOT producer.cancel_requested
    AND z.attempt=producer.attempt AND z.fencing_token=producer.fencing_token
    AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token
    AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
    AND z.outputs->0->>'artifact_id'=a.id::text AND z.outputs->0->>'sha256'=a.sha256
    AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND params.state='ready' AND params.verified_at IS NOT NULL AND params.media_type='application/json'
    AND params.bytes BETWEEN 2 AND 8388608 AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null')
    AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
    AND EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=p_tenant AND input.project_id=w.project_id
      AND input.task_id=producer.id AND input.artifact_id=params.id AND input.sha256=params.sha256 AND input.contract='Params')
    AND EXISTS(SELECT 1 FROM photo.inbox inbox WHERE inbox.tenant_id=p_tenant AND inbox.consumer='v2-adopt'
      AND inbox.event_id=photo.v2_adoption_event_id(producer.id,z.id)))));
$$;
ALTER FUNCTION photo.semantic_review_sources_current(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_review_sources_current(uuid,uuid) FROM PUBLIC;

CREATE FUNCTION photo.semantic_review_plan_sources(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(run_id uuid,revision bigint,task_id uuid,result_id uuid,contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT source.* FROM photo.semantic_review_plan_sources_pre_current(p_tenant,p_workflow,p_actor,p_session) source
 WHERE photo.semantic_review_sources_current(p_tenant,p_workflow);
$$;
ALTER FUNCTION photo.semantic_review_plan_sources(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_review_plan_sources(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_review_plan_sources(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
