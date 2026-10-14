-- +goose Up
-- Reuse the insert-only, tenant-purged provenance registry with a distinct target anchor.
-- No new DELETE privilege and no widening of generic/source protection authority.
-- +goose StatementBegin
CREATE FUNCTION photo.register_semantic_target_defect_protection(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_expected jsonb,p_document uuid,p_root uuid,p_root_sha text,p_body text,p_report text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c record; r photo.runs%ROWTYPE; cfg jsonb; binding jsonb; expected_anchor jsonb; mask jsonb; upload photo.v2_draft_uploads%ROWTYPE; saved photo.semantic_native_protections%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 OR octet_length(p_report) NOT BETWEEN 2 AND 1048576
 OR photo.lock_semantic_target_defect_result(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task,p_expected) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO c FROM photo.semantic_target_defect_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task);
 IF NOT FOUND OR c.result_id::text IS DISTINCT FROM p_expected->>'result_id' THEN RETURN false;END IF;
 SELECT x.* INTO r FROM photo.runs x JOIN photo.tasks t ON t.tenant_id=x.tenant_id AND t.run_id=x.id WHERE t.tenant_id=p_tenant AND t.id=p_task FOR SHARE OF x;
 IF NOT FOUND OR r.status<>'completed' THEN RETURN false;END IF;
 cfg:=r.config_snapshot->'v27_target_defect_prepare'; binding:=cfg->'source';
 expected_anchor:=jsonb_build_object('schema_version',1,'kind','target_defect','parent_run_id',p_parent,'request_sha',p_request_sha,'target_namespace',cfg->'target_namespace','target_ordinal',cfg->'target_ordinal','target_manifest_sha',cfg->'manifest_sha');
 IF p_document IS DISTINCT FROM photo.semantic_uuid_v5((cfg->>'target_namespace')::uuid,'semantic.defect.protection:reference')
 OR encode(public.digest(convert_to(p_report,'UTF8'),'sha256'),'hex') IS DISTINCT FROM c.outputs->4->>'sha256'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_root_sha THEN RETURN false;END IF;
 mask:=p_body::jsonb;
 IF mask IS DISTINCT FROM p_report::jsonb->'protection' OR mask->'protected' IS DISTINCT FROM 'true'::jsonb OR mask->'artifact' IS DISTINCT FROM c.outputs->1 THEN RETURN false;END IF;
 SELECT * INTO upload FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND document_id=p_document FOR SHARE;
 IF NOT FOUND OR upload.project_id IS DISTINCT FROM p_project OR upload.asset_version_id IS DISTINCT FROM (binding->>'asset_version_id')::uuid
 OR upload.artifact_id IS DISTINCT FROM p_root OR upload.sha256 IS DISTINCT FROM p_root_sha OR upload.state<>'ready' OR upload.upload_contract<>'Document' OR upload.bytes<>octet_length(p_body) THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_root AND sha256=p_root_sha AND state='ready' AND verified_at IS NOT NULL
 AND output_role='document' AND media_type='application/json' AND bytes=upload.bytes AND bucket=upload.bucket AND object_key='tenants/'||p_tenant::text||'/v2-drafts/'||p_root::text
 AND object_version=upload.object_version AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 INSERT INTO photo.semantic_native_protections(tenant_id,project_id,document_id,asset_version_id,source_id,source_sha,parent_run_id,task_id,result_id,root_id,root_sha,pixels_id,pixels_sha,report_id,report_sha,anchor)
 VALUES(p_tenant,p_project,p_document,(binding->>'asset_version_id')::uuid,(binding->'source'->>'artifact_id')::uuid,binding->'source'->>'sha256',p_parent,p_task,c.result_id,p_root,p_root_sha,(c.outputs->1->>'artifact_id')::uuid,c.outputs->1->>'sha256',(c.outputs->4->>'artifact_id')::uuid,c.outputs->4->>'sha256',expected_anchor)
 ON CONFLICT(tenant_id,document_id) DO NOTHING;
 SELECT * INTO saved FROM photo.semantic_native_protections WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN saved.project_id=p_project AND saved.asset_version_id=(binding->>'asset_version_id')::uuid AND saved.source_id=(binding->'source'->>'artifact_id')::uuid AND saved.source_sha=binding->'source'->>'sha256'
 AND saved.parent_run_id=p_parent AND saved.task_id=p_task AND saved.result_id=c.result_id AND saved.root_id=p_root AND saved.root_sha=p_root_sha
 AND saved.pixels_id=(c.outputs->1->>'artifact_id')::uuid AND saved.pixels_sha=c.outputs->1->>'sha256'
 AND saved.report_id=(c.outputs->4->>'artifact_id')::uuid AND saved.report_sha=c.outputs->4->>'sha256' AND saved.anchor=expected_anchor;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.register_semantic_target_defect_protection(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_semantic_target_defect_protection(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_semantic_target_defect_protection(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,uuid,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_target_defect_protection_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c record; r photo.runs%ROWTYPE; cfg jsonb; binding jsonb; expected_anchor jsonb;
BEGIN
 SELECT * INTO c FROM photo.semantic_target_defect_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT x.* INTO r FROM photo.runs x JOIN photo.tasks t ON t.tenant_id=x.tenant_id AND t.run_id=x.id WHERE t.tenant_id=p_tenant AND t.id=p_task;
 IF NOT FOUND OR r.status<>'completed' THEN RETURN;END IF;
 cfg:=r.config_snapshot->'v27_target_defect_prepare'; binding:=cfg->'source';
 expected_anchor:=jsonb_build_object('schema_version',1,'kind','target_defect','parent_run_id',p_parent,'request_sha',p_request_sha,'target_namespace',cfg->'target_namespace','target_ordinal',cfg->'target_ordinal','target_manifest_sha',cfg->'manifest_sha');
 RETURN QUERY SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.semantic_native_protections s JOIN photo.v2_draft_uploads u ON u.tenant_id=s.tenant_id AND u.document_id=s.document_id
 JOIN photo.artifacts a ON a.tenant_id=s.tenant_id AND a.id=s.root_id
 WHERE s.tenant_id=p_tenant AND s.project_id=p_project AND s.parent_run_id=p_parent AND s.task_id=p_task AND s.result_id=c.result_id AND s.anchor=expected_anchor
 AND s.document_id=photo.semantic_uuid_v5((cfg->>'target_namespace')::uuid,'semantic.defect.protection:reference')
 AND s.asset_version_id=(binding->>'asset_version_id')::uuid AND s.source_id=(binding->'source'->>'artifact_id')::uuid AND s.source_sha=binding->'source'->>'sha256'
 AND s.pixels_id=(c.outputs->1->>'artifact_id')::uuid AND s.pixels_sha=c.outputs->1->>'sha256' AND s.report_id=(c.outputs->4->>'artifact_id')::uuid AND s.report_sha=c.outputs->4->>'sha256'
 AND u.project_id=p_project AND u.asset_version_id=s.asset_version_id AND u.artifact_id=a.id AND u.sha256=a.sha256 AND u.state='ready' AND u.upload_contract='Document'
 AND a.sha256=s.root_sha AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document' AND a.media_type='application/json'
 AND a.bytes=u.bytes AND a.bucket=u.bucket AND a.object_key='tenants/'||p_tenant::text||'/v2-drafts/'||a.id::text AND a.object_version=u.object_version AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
END $$;
ALTER FUNCTION photo.semantic_target_defect_protection_object(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_defect_protection_object(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_target_defect_protection_object(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_target_defect_protection(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_expected jsonb,p_root jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE o record;actual jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_root) IS DISTINCT FROM 'object' OR octet_length(p_root::text)>8192
 OR photo.lock_semantic_target_defect_result(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task,p_expected) IS NOT TRUE THEN RETURN false;END IF;
 PERFORM document_id FROM photo.semantic_native_protections WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY document_id FOR SHARE;
 PERFORM document_id FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND artifact_id=(p_root->'ref'->>'artifact_id')::uuid FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(p_root->'ref'->>'artifact_id')::uuid FOR SHARE;
 SELECT * INTO o FROM photo.semantic_target_defect_protection_object(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task);
 IF NOT FOUND THEN RETURN false;END IF;
 actual:=jsonb_build_object('ref',jsonb_build_object('artifact_id',o.artifact_id,'sha256',o.sha256),'bucket',o.bucket,'key',o.object_key,'version',o.object_version,'bytes',o.bytes,'media',o.media_type);
 RETURN actual=p_root;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_target_defect_protection(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_defect_protection(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_target_defect_protection(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
