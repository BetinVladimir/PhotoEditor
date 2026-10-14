-- +goose Up
-- Typed manual roots are pinned by a live saved V28 owner, not arbitrary refs.
-- +goose StatementBegin
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) RENAME TO v2_draft_artifact_scope_pre_v28_manual;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope_pre_v28_manual(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM photo_api;
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;owner jsonb;pin jsonb;
BEGIN
 IF photo.v2_draft_artifact_scope_pre_v28_manual(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,p_parent,p_parent_sha) IS TRUE THEN RETURN true;END IF;
 IF p_contract IS NULL OR p_contract NOT IN('RasterizedMask','ManualMask','Params')
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 -- Pre-owner access requires the canonical original producer, accepted source,
 -- and (for vectors) exact parent plus both producer input declarations.
 IF EXISTS(SELECT 1 FROM photo.artifacts root
  JOIN photo.tasks t ON t.tenant_id=root.tenant_id AND t.id=root.created_by_task_id
  JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.id=t.canonical_result_id
   AND result.task_id=t.id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
  JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id
   AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token AND attempt.finished_at IS NOT NULL
  JOIN photo.artifacts params ON params.tenant_id=t.tenant_id AND params.id=t.params_artifact_id
  JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
  JOIN photo.interactive_tasks i ON i.tenant_id=t.tenant_id AND i.task_id=t.id
  JOIN photo.asset_versions v ON v.tenant_id=t.tenant_id AND v.id=p_version AND v.master_artifact_id=i.source_artifact_id
  JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
  JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
  JOIN photo.artifacts item ON item.tenant_id=t.tenant_id AND item.id=p_artifact AND item.sha256=p_sha
  WHERE root.tenant_id=p_tenant AND root.output_role='rasterized_mask'
   AND t.operation='mask.rasterize' AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
   AND r.kind='mask' AND r.graph_sealed AND NOT r.cancel_requested
   AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified' AND v.review_state='accepted'
   AND source.state='ready' AND source.verified_at IS NOT NULL AND source.media_type='application/json'
   AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null') AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
   AND r.config_snapshot->>'asset_version_id'=p_version::text AND r.config_snapshot->>'source_sha256'=v.content_hash
   AND params.state='ready' AND params.verified_at IS NOT NULL AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
   AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null') AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
   AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
   AND jsonb_typeof(result.outputs)='array'
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
   AND root.state='ready' AND root.verified_at IS NOT NULL AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
   AND root.object_version IS NOT NULL AND root.object_version NOT IN('','null') AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
   AND item.state='ready' AND item.verified_at IS NOT NULL AND item.media_type='application/json' AND item.bytes BETWEEN 2 AND 1048576
   AND item.object_version IS NOT NULL AND item.object_version NOT IN('','null') AND (item.expires_at IS NULL OR item.expires_at>clock_timestamp())
   AND ((p_contract='RasterizedMask' AND root.id=p_artifact AND root.sha256=p_sha AND p_parent IS NULL AND p_parent_sha IS NULL)
    OR (p_contract='Params' AND item.id=params.id AND item.sha256=params.sha256
     AND ((p_parent IS NULL AND p_parent_sha IS NULL) OR (p_parent=root.id AND p_parent_sha=root.sha256)))
    OR (p_contract='ManualMask' AND ((root.id=p_parent AND root.sha256=p_parent_sha) OR (params.id=p_parent AND params.sha256=p_parent_sha)) AND p_artifact NOT IN(root.id,params.id)
     AND jsonb_typeof(t.inputs)='array' AND jsonb_typeof(i.declared_inputs)='array'
     AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha))
     AND i.declared_inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha))))) THEN RETURN true;END IF;
 FOR candidate IN SELECT r.config_snapshot->'v28_local_portrait' AS request
  FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.kind='v2' AND r.status='draft'
   AND NOT r.graph_sealed AND NOT r.cancel_requested AND r.created_by=p_actor
   AND r.config_snapshot->'v28_local_portrait'->>'project_id'=p_project::text
   AND r.config_snapshot->'v28_local_portrait'->>'auth_session_id'=p_session::text
   AND r.config_snapshot->'v28_local_portrait'->'intent'->'binding'->>'asset_version_id'=p_version::text
 LOOP
  owner:=candidate.request;
  IF p_contract='RasterizedMask' THEN
   IF p_parent IS NOT NULL OR p_parent_sha IS NOT NULL THEN CONTINUE;END IF;
   pin:=owner->'manual_root';
  ELSIF p_contract='Params' THEN
   pin:=owner->'original_params';
   IF ((p_parent IS NULL AND p_parent_sha IS NULL) OR (p_parent::text=owner->'manual_root'->>'artifact_id' AND p_parent_sha=owner->'manual_root'->>'sha256')) IS DISTINCT FROM true THEN CONTINUE;END IF;
  ELSE
   pin:=owner->'vectors';
   IF ((p_parent::text=owner->'manual_root'->>'artifact_id' AND p_parent_sha=owner->'manual_root'->>'sha256')
    OR (p_parent::text=owner->'original_params'->>'artifact_id' AND p_parent_sha=owner->'original_params'->>'sha256')) IS DISTINCT FROM true THEN CONTINUE;END IF;
  END IF;
  IF pin->>'artifact_id' IS DISTINCT FROM p_artifact::text OR pin->>'sha256' IS DISTINCT FROM p_sha THEN CONTINUE;END IF;
  IF photo.v28_local_owner_request_scope(owner) IS DISTINCT FROM true THEN CONTINUE;END IF;
  IF EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN true;END IF;
 END LOOP;
 RETURN false;
END $$;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
