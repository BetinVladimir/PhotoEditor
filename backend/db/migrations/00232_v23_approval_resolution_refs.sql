-- Learning approval pins its exact profile and source descriptor as immutable
-- document closure refs. The existing reference table already participates in
-- artifact GC and tenant purge; no parallel lifecycle is introduced.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.approve_v23_draft_with_sources(
 p_tenant uuid,p_asset_version uuid,p_expected bigint,p_document uuid,
 p_actor uuid,p_session uuid,p_resolved_artifact uuid,p_resolved_sha text,
 p_learning_version uuid,p_model_artifact uuid,p_model_sha text,
 p_descriptor_artifact uuid,p_descriptor_sha text,p_analysis uuid,
 p_descriptor_ordinal integer) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb; document_project uuid; model_purpose text;
 descriptor_purpose text;
BEGIN
 IF p_learning_version IS NULL THEN
  IF p_model_artifact IS NOT NULL OR p_model_sha IS NOT NULL OR
   p_descriptor_artifact IS NOT NULL OR p_descriptor_sha IS NOT NULL OR
   p_analysis IS NOT NULL OR p_descriptor_ordinal IS NOT NULL THEN
   RAISE EXCEPTION 'V23_APPROVAL_SOURCES_INVALID' USING ERRCODE='22023';
  END IF;
 ELSE
  IF p_model_artifact IS NULL OR p_descriptor_artifact IS NULL OR
   p_analysis IS NULL OR coalesce(p_model_sha,'') !~ '^[0-9a-f]{64}$' OR
   coalesce(p_descriptor_sha,'') !~ '^[0-9a-f]{64}$' OR
   p_descriptor_ordinal IS NULL OR p_descriptor_ordinal NOT BETWEEN 0 AND 4999
   THEN RAISE EXCEPTION 'V23_APPROVAL_SOURCES_INVALID' USING ERRCODE='22023';
  END IF;
 END IF;
 result:=photo.approve_v23_draft(p_tenant,p_asset_version,p_expected,
  p_document,p_actor,p_session,p_resolved_artifact,p_resolved_sha,
  p_learning_version);
 IF p_learning_version IS NULL THEN RETURN result; END IF;
 SELECT project_id INTO document_project FROM photo.v2_documents
  WHERE tenant_id=p_tenant AND id=p_document;
 IF document_project IS NULL OR NOT EXISTS(
  SELECT 1 FROM photo.model_use_links used
  JOIN photo.learning_versions version ON version.tenant_id=used.tenant_id
   AND version.project_id=used.project_id AND version.id=used.version_id
  JOIN photo.artifacts model ON model.tenant_id=version.tenant_id
   AND model.id=version.model_artifact_id
  WHERE used.tenant_id=p_tenant AND used.project_id=document_project
   AND used.document_id=p_document AND used.version_id=p_learning_version
   AND version.model_artifact_id=p_model_artifact
   AND model.sha256=p_model_sha AND model.state='ready'
   AND model.output_role='document' AND model.verified_at IS NOT NULL
   AND model.object_version IS NOT NULL
   AND (model.expires_at IS NULL OR model.expires_at>clock_timestamp()))
  THEN RAISE EXCEPTION 'V23_APPROVAL_MODEL_INVALID' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(
  SELECT 1 FROM photo.session_analyses analysis
  JOIN photo.scene_groups group_row ON group_row.tenant_id=analysis.tenant_id
   AND group_row.project_id=analysis.project_id
   AND group_row.analysis_id=analysis.id
  JOIN photo.scene_members member ON member.tenant_id=group_row.tenant_id
   AND member.project_id=group_row.project_id AND member.group_id=group_row.id
  JOIN photo.v2_workflows workflow ON workflow.tenant_id=analysis.tenant_id
   AND workflow.project_id=analysis.project_id
   AND workflow.id=analysis.workflow_id
  JOIN photo.runs run ON run.tenant_id=workflow.tenant_id
   AND run.id=workflow.run_id
  JOIN photo.tasks task ON task.tenant_id=workflow.tenant_id
   AND task.run_id=workflow.run_id AND task.operation='session.describe'
   AND task.state='succeeded'
  JOIN photo.task_results output ON output.tenant_id=task.tenant_id
   AND output.task_id=task.id AND output.id=task.canonical_result_id
   AND output.attempt=task.attempt
   AND output.fencing_token=task.fencing_token
  JOIN photo.artifacts descriptor ON descriptor.tenant_id=task.tenant_id
   AND descriptor.id=p_descriptor_artifact
   AND descriptor.sha256=p_descriptor_sha
  WHERE analysis.tenant_id=p_tenant AND analysis.project_id=document_project
   AND analysis.id=p_analysis AND member.asset_version_id=p_asset_version
   AND member.snapshot_ordinal=p_descriptor_ordinal
   AND workflow.workflow_type='session'
   AND run.status IN ('waiting_review','completed')
   AND output.outputs=jsonb_build_array(jsonb_build_object(
    'artifact_id',p_descriptor_artifact,'sha256',p_descriptor_sha))
   AND descriptor.created_by_task_id=task.id
   AND descriptor.produced_attempt=task.attempt
   AND descriptor.produced_fence=task.fencing_token
   AND descriptor.state='ready' AND descriptor.output_role='document'
   AND descriptor.verified_at IS NOT NULL
   AND descriptor.object_version IS NOT NULL
   AND (descriptor.expires_at IS NULL OR
    descriptor.expires_at>clock_timestamp()))
  THEN RAISE EXCEPTION 'V23_APPROVAL_DESCRIPTOR_INVALID' USING ERRCODE='23514';
 END IF;
 model_purpose:=encode(public.digest('Approval.learning_model:CandidateProfile','sha256'),'hex');
 descriptor_purpose:=encode(public.digest('Approval.source_descriptor:DescriptorShard','sha256'),'hex');
 IF EXISTS(SELECT 1 FROM photo.v2_document_refs ref
  WHERE ref.tenant_id=p_tenant AND ref.document_id=p_document
   AND ref.purpose=model_purpose AND
   (ref.artifact_id<>p_model_artifact OR ref.sha256<>p_model_sha OR ref.ordinal<>0)) OR
  EXISTS(SELECT 1 FROM photo.v2_document_refs ref
   WHERE ref.tenant_id=p_tenant AND ref.document_id=p_document
    AND ref.purpose=descriptor_purpose AND
    (ref.artifact_id<>p_descriptor_artifact OR ref.sha256<>p_descriptor_sha OR
     ref.ordinal<>p_descriptor_ordinal)) THEN
  RAISE EXCEPTION 'V23_APPROVAL_SOURCES_CONFLICT' USING ERRCODE='40001';
 END IF;
 INSERT INTO photo.v2_document_refs(tenant_id,project_id,document_id,
  artifact_id,sha256,purpose,ordinal)
 VALUES(p_tenant,document_project,p_document,p_model_artifact,p_model_sha,
  model_purpose,0),
  (p_tenant,document_project,p_document,p_descriptor_artifact,
   p_descriptor_sha,descriptor_purpose,p_descriptor_ordinal)
 ON CONFLICT(tenant_id,document_id,purpose,ordinal) DO NOTHING;
 RETURN result;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.approve_v23_draft_with_sources(uuid,uuid,bigint,uuid,uuid,
 uuid,uuid,text,uuid,uuid,text,uuid,text,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.approve_v23_draft_with_sources(uuid,uuid,bigint,uuid,
 uuid,uuid,uuid,text,uuid,uuid,text,uuid,text,uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.approve_v23_draft_with_sources(uuid,uuid,bigint,
 uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,text,uuid,integer) TO photo_api;
REVOKE EXECUTE ON FUNCTION photo.approve_v23_draft(uuid,uuid,bigint,uuid,uuid,
 uuid,uuid,text,uuid) FROM photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
