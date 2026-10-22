-- +goose Up
-- Repeat live shown-source authority immediately before media plan activation.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_media_source_live(p_event uuid,p_inputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.media_retry_jobs%ROWTYPE;v photo.asset_versions%ROWTYPE;source photo.runs%ROWTYPE;
 target photo.semantic_targets%ROWTYPE;t photo.tasks%ROWTYPE;expected jsonb;
BEGIN
 SELECT * INTO j FROM photo.media_retry_jobs WHERE event_id=p_event;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO v FROM photo.asset_versions WHERE tenant_id=j.tenant_id AND id=j.asset_version_id;
 SELECT * INTO source FROM photo.runs WHERE tenant_id=j.tenant_id AND id=v.created_by_run_id;
 IF source.config_snapshot ? 'shown_semantic' THEN
  IF NOT EXISTS(SELECT 1 FROM photo.assets a JOIN photo.projects p ON p.tenant_id=a.tenant_id AND p.id=a.project_id
   JOIN photo.tenants tenant ON tenant.id=a.tenant_id WHERE a.tenant_id=j.tenant_id AND a.id=v.asset_id
   AND a.deleted_at IS NULL AND a.upload_state='verified' AND p.archived_at IS NULL AND tenant.status='active') THEN RETURN false;END IF;
  SELECT * INTO target FROM photo.semantic_targets WHERE tenant_id=j.tenant_id AND child_run_id=source.id
   AND asset_version_id=v.parent_asset_version_id
   AND v.id=photo.semantic_uuid_v5(photo.semantic_uuid_v5(source.id,'semantic.adopt_shown_exposure:'||id::text),'V27.ShownAppliedVersion');
  IF NOT FOUND OR target.state<>'applied' OR source.kind<>'apply' OR source.status<>'completed' OR source.cancel_requested
   OR v.review_state<>'accepted' OR j.origin<>'apply' OR j.origin_run_id IS DISTINCT FROM source.id THEN RETURN false;END IF;
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=j.tenant_id AND run_id=source.id
   AND id=photo.semantic_uuid_v5(source.id,'semantic.adopt_shown_exposure:'||target.id::text);
  IF NOT FOUND OR t.operation<>'semantic.adopt_shown_exposure' OR t.operation_version<>'1' OR t.state<>'succeeded'
   OR t.cancel_requested OR t.canonical_result_id IS NULL
   OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=j.tenant_id AND i.consumer='v2-adopt'
    AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
   OR NOT EXISTS(SELECT 1 FROM photo.asset_version_sources lineage WHERE lineage.tenant_id=j.tenant_id
    AND lineage.version_id=v.id AND lineage.source_version_id=target.asset_version_id AND lineage.role='semantic_source' AND lineage.ordinal=0)
   OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=j.tenant_id AND z.id=t.canonical_result_id
    AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
    AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',v.master_artifact_id,'sha256',v.content_hash)))
   OR jsonb_typeof(v.metadata->'pixels') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
  expected:=jsonb_build_array(jsonb_build_object('artifact_id',v.master_artifact_id,'sha256',v.content_hash),v.metadata->'pixels');
  IF v.metadata->'alpha' IS NOT NULL AND v.metadata->'alpha'<>'null'::jsonb THEN expected:=expected||jsonb_build_array(v.metadata->'alpha');END IF;
  IF p_inputs IS DISTINCT FROM expected THEN RETURN false;END IF;
 END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.semantic_shown_media_source_live(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_media_source_live(uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_scheduler;
ALTER FUNCTION photo.seal_media_retry_plan(uuid,uuid,uuid) RENAME TO seal_media_retry_plan_pre_shown_source;
REVOKE ALL ON FUNCTION photo.seal_media_retry_plan_pre_shown_source(uuid,uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_media_retry_plan(p_event uuid,p_token uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node photo.media_retry_nodes%ROWTYPE;
BEGIN
 -- Serialize with tenant/project/asset lifecycle before the job-row lock held by old seal.
 PERFORM tenant.id FROM photo.media_retry_jobs j JOIN photo.tenants tenant ON tenant.id=j.tenant_id WHERE j.event_id=p_event FOR UPDATE OF tenant;
 PERFORM p.id FROM photo.media_retry_jobs j JOIN photo.asset_versions v ON v.tenant_id=j.tenant_id AND v.id=j.asset_version_id
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id JOIN photo.projects p ON p.tenant_id=a.tenant_id AND p.id=a.project_id
 WHERE j.event_id=p_event FOR UPDATE OF p;
 PERFORM a.id FROM photo.media_retry_jobs j JOIN photo.asset_versions v ON v.tenant_id=j.tenant_id AND v.id=j.asset_version_id
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE j.event_id=p_event FOR UPDATE OF a;
 FOR node IN SELECT * FROM photo.media_retry_nodes WHERE event_id=p_event AND run_id=p_run ORDER BY id LOOP
  IF photo.semantic_shown_media_source_live(p_event,node.inputs) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 RETURN photo.seal_media_retry_plan_pre_shown_source(p_event,p_token,p_run);
END $$;
ALTER FUNCTION photo.seal_media_retry_plan(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_media_retry_plan(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.seal_media_retry_plan(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
