-- +goose Up
-- New native-input registration; existing immutable provenance table and purge unchanged.
-- +goose StatementBegin
CREATE FUNCTION photo.register_semantic_native_input_protection(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_task uuid,p_result uuid,p_anchor jsonb,p_document uuid,p_root uuid,p_root_sha text,p_body text,p_report text,p_completed jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE catalog record; upload photo.v2_draft_uploads%ROWTYPE; saved photo.semantic_native_protections%ROWTYPE; mask jsonb; expected_parent uuid;
BEGIN
 IF octet_length(p_body) NOT BETWEEN 2 AND 1048576 OR octet_length(p_report) NOT BETWEEN 2 AND 1048576
 OR photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) IS NOT TRUE
 OR photo.lock_semantic_proposal_person_inventory(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR photo.lock_semantic_native_defect_result(p_tenant,p_project,p_actor,p_session,(p_anchor->>'parent_run_id')::uuid,p_anchor->>'request_sha',p_task,p_completed) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO catalog FROM photo.semantic_native_defect_result_catalog(p_tenant,p_project,p_actor,p_session,(p_anchor->>'parent_run_id')::uuid,p_anchor->>'request_sha',p_task);
 IF NOT FOUND OR catalog.result_id IS DISTINCT FROM p_result THEN RETURN false;END IF;
 expected_parent:=(p_anchor->>'parent_run_id')::uuid;
 IF jsonb_typeof(p_anchor) IS DISTINCT FROM 'object' OR octet_length(p_anchor::text)>8192
 OR (p_anchor-ARRAY['parent_run_id','request_sha','auth_session_id','source','intent','snapshot'])<>'{}'
 OR p_anchor->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR p_anchor->'source'->>'asset_version_id' IS DISTINCT FROM p_version::text
 OR p_anchor->'source'->'source' IS DISTINCT FROM jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha)
 OR NOT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.tenant_id=p_tenant AND t.id=p_task
 AND r.config_snapshot->'v27_defect_native_prepare' @> p_anchor) THEN RETURN false;END IF;

 IF p_document IS DISTINCT FROM photo.semantic_uuid_v5(expected_parent,'semantic.defect.protection:'||p_task::text)
 OR encode(public.digest(convert_to(p_report,'UTF8'),'sha256'),'hex') IS DISTINCT FROM catalog.outputs->4->>'sha256'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_root_sha THEN RETURN false;END IF;
 mask:=p_body::jsonb;
 IF mask IS DISTINCT FROM p_report::jsonb->'protection' OR mask->'protected' IS DISTINCT FROM 'true'::jsonb
 OR mask->'artifact' IS DISTINCT FROM catalog.outputs->1 THEN RETURN false;END IF;
 SELECT * INTO upload FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND document_id=p_document FOR SHARE;
 IF NOT FOUND OR upload.project_id<>p_project OR upload.asset_version_id<>p_version OR upload.artifact_id<>p_root OR upload.sha256<>p_root_sha
 OR upload.state<>'ready' OR upload.upload_contract<>'Document' OR upload.bytes<>octet_length(p_body) THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_root AND sha256=p_root_sha AND state='ready'
 AND verified_at IS NOT NULL AND output_role='document' AND media_type='application/json' AND bytes=upload.bytes
 AND bucket=upload.bucket AND object_version=upload.object_version AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 INSERT INTO photo.semantic_native_protections(tenant_id,project_id,document_id,asset_version_id,source_id,source_sha,parent_run_id,task_id,result_id,root_id,root_sha,pixels_id,pixels_sha,report_id,report_sha,anchor)
 VALUES(p_tenant,p_project,p_document,p_version,p_source,p_source_sha,expected_parent,p_task,p_result,p_root,p_root_sha,(catalog.outputs->1->>'artifact_id')::uuid,catalog.outputs->1->>'sha256',(catalog.outputs->4->>'artifact_id')::uuid,catalog.outputs->4->>'sha256',p_anchor)
 ON CONFLICT(tenant_id,document_id) DO NOTHING;
 -- Records are insert-only; ON CONFLICT serializes replay. Scope locks block tenant purge.
 SELECT * INTO saved FROM photo.semantic_native_protections WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN saved.project_id=p_project AND saved.asset_version_id=p_version AND saved.source_id=p_source AND saved.source_sha=p_source_sha
 AND saved.parent_run_id=expected_parent AND saved.task_id=p_task AND saved.result_id=p_result AND saved.root_id=p_root AND saved.root_sha=p_root_sha
 AND saved.pixels_id=(catalog.outputs->1->>'artifact_id')::uuid AND saved.pixels_sha=catalog.outputs->1->>'sha256'
 AND saved.report_id=(catalog.outputs->4->>'artifact_id')::uuid AND saved.report_sha=catalog.outputs->4->>'sha256' AND saved.anchor=p_anchor;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.register_semantic_native_input_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_semantic_native_input_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_semantic_native_input_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
