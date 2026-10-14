-- +goose Up
-- Recheck registered frozen learning against live model consent or the exact
-- session.describe producer under a leased export. No HTTP session is borrowed.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_learning_occurrence(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_root uuid,p_root_sha text,p_version uuid,p_source uuid,p_source_sha text,
 p_learning jsonb,p_lock text,p_contract text,p_child uuid,p_child_sha text,
 p_bucket text,p_object_key text,p_object_version text,p_bytes bigint,
 p_root_object_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;root_upload photo.v2_draft_uploads%ROWTYPE;
 model_id uuid;model_sha text;descriptor_id uuid;descriptor_sha text;
 analysis_id uuid;learning_id uuid;ordinal integer;
BEGIN
 IF p_root IS NULL OR p_child IS NULL OR p_version IS NULL OR p_source IS NULL
  OR coalesce(p_root_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_child_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_contract IS NULL OR p_contract NOT IN ('CandidateProfile','DescriptorShard')
  OR jsonb_typeof(p_learning) IS DISTINCT FROM 'object'
  OR (p_learning ?& ARRAY['version_id','model','descriptor','analysis_id','ordinal']) IS DISTINCT FROM true
  OR (p_learning-'version_id'-'model'-'descriptor'-'analysis_id'-'ordinal')<>'{}'::jsonb
  OR (p_contract='DescriptorShard' AND coalesce(p_lock,'') !~ '^[0-9a-f]{64}$')
  OR coalesce(p_bucket,'')='' OR coalesce(p_object_key,'')=''
  OR coalesce(p_object_version,'') IN ('','null')
  OR coalesce(p_root_object_version,'') IN ('','null')
  OR p_bytes NOT BETWEEN 2 AND 8388608 THEN RETURN false;END IF;
 learning_id:=(p_learning->>'version_id')::uuid;
 model_id:=(p_learning#>>'{model,artifact_id}')::uuid;
 model_sha:=p_learning#>>'{model,sha256}';
 descriptor_id:=(p_learning#>>'{descriptor,artifact_id}')::uuid;
 descriptor_sha:=p_learning#>>'{descriptor,sha256}';
 analysis_id:=(p_learning->>'analysis_id')::uuid;
 ordinal:=(p_learning->>'ordinal')::integer;
 IF learning_id IS NULL OR model_id IS NULL OR descriptor_id IS NULL
  OR analysis_id IS NULL OR ordinal NOT BETWEEN 0 AND 4999
  OR coalesce(model_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(descriptor_sha,'') !~ '^[0-9a-f]{64}$'
  OR (p_contract='CandidateProfile' AND (p_child<>model_id OR p_child_sha<>model_sha))
  OR (p_contract='DescriptorShard' AND (p_child<>descriptor_id OR p_child_sha<>descriptor_sha))
 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_root,p_root_sha,'FrozenRenderRecipe');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_child,p_child_sha,p_contract);
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR photo.v30_export_adoption_source_ready(
  p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 SELECT root.* INTO root_upload FROM photo.v2_draft_uploads root
 JOIN photo.artifacts artifact ON artifact.tenant_id=root.tenant_id
  AND artifact.id=root.artifact_id AND artifact.sha256=root.sha256
 JOIN photo.snapshot_items item ON item.tenant_id=root.tenant_id
  AND item.snapshot_id=w.input_snapshot_id
  AND item.asset_version_id=root.asset_version_id
  AND item.artifact_id=root.render_source_id AND item.sha256=root.render_source_sha
 JOIN photo.asset_versions version ON version.tenant_id=root.tenant_id
  AND version.id=root.asset_version_id
  AND version.master_artifact_id=item.artifact_id AND version.content_hash=item.sha256
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
  AND asset.id=version.asset_id AND asset.project_id=root.project_id
 WHERE root.tenant_id=p_tenant AND root.project_id=w.project_id
  AND root.asset_version_id=p_version AND root.artifact_id=p_root
  AND root.sha256=p_root_sha AND root.state='ready'
  AND root.render_registered_at IS NOT NULL AND root.render_learning_checked
  AND root.render_learning=p_learning
  AND root.render_learning_model_lock ~ '^[0-9a-f]{64}$'
  AND (p_contract='CandidateProfile' OR root.render_learning_model_lock=p_lock)
  AND root.render_source_id=p_source AND root.render_source_sha=p_source_sha
  AND artifact.state='ready' AND artifact.output_role='document'
  AND artifact.media_type='application/json' AND artifact.verified_at IS NOT NULL
  AND artifact.object_version=p_root_object_version
  AND artifact.object_version=root.object_version
  AND artifact.bytes=root.bytes AND artifact.bytes BETWEEN 2 AND 1048576
  AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp())
  AND version.review_state='accepted' AND asset.deleted_at IS NULL
  AND asset.upload_state='verified'
 FOR SHARE OF root,artifact,item,version,asset;
 IF root_upload.artifact_id IS NULL THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts child
  WHERE child.tenant_id=p_tenant AND child.id=p_child AND child.sha256=p_child_sha
   AND child.bucket=p_bucket AND child.object_key=p_object_key
   AND child.object_version=p_object_version AND child.bytes=p_bytes
   AND child.state='ready' AND child.output_role='document'
   AND child.media_type='application/json' AND child.verified_at IS NOT NULL
   AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp()))
 THEN RETURN false;END IF;
 IF p_contract='CandidateProfile' THEN
  RETURN EXISTS(SELECT 1 FROM photo.v2_documents document
   JOIN photo.v2_document_streams stream ON stream.tenant_id=document.tenant_id
    AND stream.id=document.stream_id
   JOIN photo.model_use_links used ON used.tenant_id=document.tenant_id
    AND used.project_id=document.project_id AND used.document_id=document.id
   JOIN photo.learning_versions version ON version.tenant_id=used.tenant_id
    AND version.project_id=used.project_id AND version.id=used.version_id
   JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
    AND profile.project_id=version.project_id AND profile.id=version.profile_id
   JOIN photo.version_eligibility eligibility ON eligibility.tenant_id=version.tenant_id
    AND eligibility.project_id=version.project_id AND eligibility.version_id=version.id
   JOIN photo.artifacts training ON training.tenant_id=version.tenant_id
    AND training.id=version.training_artifact_id
   WHERE document.tenant_id=p_tenant AND document.project_id=w.project_id
    AND document.sha256=root_upload.render_authored_sha
    AND document.document_type='EditingRecipeV3'
    AND stream.project_id=w.project_id AND stream.asset_version_id=p_version
    AND stream.stream_type='draft' AND version.id=learning_id
    AND version.model_artifact_id=p_child AND profile.active_version_id=version.id
    AND eligibility.status='eligible' AND training.state='ready'
    AND training.output_role='document' AND training.media_type='application/json'
    AND training.verified_at IS NOT NULL
    AND training.object_version IS NOT NULL AND training.object_version NOT IN('','null')
    AND (training.expires_at IS NULL OR training.expires_at>clock_timestamp())
    AND photo.v23_learning_version_consents_live(p_tenant,version.id));
 END IF;
 RETURN EXISTS(SELECT 1 FROM photo.session_analyses analysis
  JOIN photo.v2_workflows origin_workflow ON origin_workflow.tenant_id=analysis.tenant_id
   AND origin_workflow.project_id=analysis.project_id
   AND origin_workflow.id=analysis.workflow_id
  JOIN photo.runs origin ON origin.tenant_id=origin_workflow.tenant_id
   AND origin.id=origin_workflow.run_id
  JOIN photo.artifacts plan ON plan.tenant_id=analysis.tenant_id
   AND plan.id=analysis.plan_artifact_id
  JOIN photo.snapshot_items item ON item.tenant_id=analysis.tenant_id
   AND item.snapshot_id=analysis.snapshot_id AND item.asset_version_id=p_version
   AND item.ordinal=ordinal AND item.artifact_id=p_source
   AND item.sha256=p_source_sha
  JOIN photo.tasks producer ON producer.tenant_id=origin.tenant_id
   AND producer.run_id=origin.id
  JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
   AND result.task_id=producer.id AND result.id=producer.canonical_result_id
   AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
  JOIN photo.artifacts descriptor ON descriptor.tenant_id=producer.tenant_id
   AND descriptor.id=p_child AND descriptor.sha256=p_child_sha
  WHERE analysis.tenant_id=p_tenant AND analysis.project_id=w.project_id
   AND analysis.id=analysis_id AND analysis.model_lock_sha=p_lock
   AND origin_workflow.workflow_type='session'
   AND origin_workflow.renderer_version='v2.2' AND origin.kind='v2'
   AND origin.graph_sealed AND NOT origin.cancel_requested
   AND origin.status IN ('waiting_review','completed')
   AND plan.state='ready' AND plan.verified_at IS NOT NULL
   AND plan.object_version IS NOT NULL AND plan.object_version NOT IN('','null')
   AND (plan.expires_at IS NULL OR plan.expires_at>clock_timestamp())
   AND producer.operation='session.describe' AND producer.state='succeeded'
   AND jsonb_typeof(result.outputs)='array' AND jsonb_array_length(result.outputs)=1
   AND result.outputs->0->>'artifact_id'=p_child::text
   AND result.outputs->0->>'sha256'=p_child_sha
   AND descriptor.created_by_task_id=producer.id
   AND descriptor.produced_attempt=producer.attempt
   AND descriptor.produced_fence=producer.fencing_token
   AND EXISTS(SELECT 1 FROM photo.scene_groups g
    JOIN photo.scene_members member ON member.tenant_id=g.tenant_id
     AND member.project_id=g.project_id AND member.group_id=g.id
    WHERE g.tenant_id=analysis.tenant_id AND g.project_id=analysis.project_id
     AND g.analysis_id=analysis.id AND member.asset_version_id=p_version
     AND member.snapshot_ordinal=ordinal));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_learning_occurrence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,uuid,text,jsonb,text,text,uuid,text,text,text,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_learning_occurrence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,uuid,text,jsonb,text,text,uuid,text,text,text,text,bigint,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_learning_occurrence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,uuid,text,jsonb,text,text,uuid,text,text,text,text,bigint,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
