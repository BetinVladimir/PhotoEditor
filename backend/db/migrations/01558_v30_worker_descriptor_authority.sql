-- +goose Up
-- Lease-scoped worker bridge to existing API render authority. No direct grants
-- on render helpers; all references must belong to this sealed export inventory.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_descriptor_authority(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text,p_evidence jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE context jsonb;project uuid;actor uuid;session uuid;proof jsonb;root jsonb;descriptor jsonb;
 selected jsonb;frame jsonb;ref jsonb;body jsonb;kind text;analysis uuid;ordinal integer;lock_sha text;root_raw text;
BEGIN
 context:=photo.v30_interchange_export_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest);
 IF context IS NULL OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'object'
 OR NOT p_evidence ?& ARRAY['root','root_jcs','descriptor','selected_binding','analysis_id','selected_ordinal','model_lock_sha256','reference','contract','frame_binding','frame_ordinal']
 OR p_evidence-'root'-'root_jcs'-'descriptor'-'selected_binding'-'analysis_id'-'selected_ordinal'-'model_lock_sha256'-'reference'-'contract'-'frame_binding'-'frame_ordinal'<>'{}'::jsonb THEN RETURN false;END IF;
 project:=(context->>'project_id')::uuid;
 SELECT r.created_by,d.v30_interchange_proof INTO actor,proof FROM photo.runs r JOIN photo.backend_documents d
 ON d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.owner_id=p_task AND d.artifact_id=p_params AND d.sha256=p_sha AND d.purpose='task_params' AND d.state='ready'
 WHERE r.tenant_id=p_tenant AND r.id=p_run;
 session:=(proof->>'auth_session_id')::uuid;
 root:=p_evidence->'root';descriptor:=p_evidence->'descriptor';selected:=p_evidence->'selected_binding';frame:=p_evidence->'frame_binding';ref:=p_evidence->'reference';kind:=p_evidence->>'contract';root_raw:=p_evidence->>'root_jcs';
 analysis:=(p_evidence->>'analysis_id')::uuid;ordinal:=(p_evidence->>'selected_ordinal')::integer;lock_sha:=p_evidence->>'model_lock_sha256';
 IF root_raw IS NULL OR octet_length(root_raw) NOT BETWEEN 2 AND 1048576 OR encode(sha256(convert_to(root_raw,'UTF8')),'hex') IS DISTINCT FROM root->>'sha256' THEN RETURN false;END IF;
 body:=root_raw::jsonb;
 IF body->'scalars'->'source' IS DISTINCT FROM selected OR body->'learning'->'descriptor' IS DISTINCT FROM descriptor
 OR body->'learning'->>'analysis_id' IS DISTINCT FROM analysis::text OR body->'learning'->>'ordinal' IS DISTINCT FROM ordinal::text
 OR body->'scalars'->>'model_lock_sha' IS DISTINCT FROM lock_sha THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND artifact_id=(root->>'artifact_id')::uuid AND sha256=root->>'sha256' AND contract='FrozenRenderRecipe')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND artifact_id=(descriptor->>'artifact_id')::uuid AND sha256=descriptor->>'sha256' AND contract='DescriptorShard')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' AND contract=kind) THEN RETURN false;END IF;
 IF photo.v26_render_model_scope(p_tenant,project,(selected->>'asset_version_id')::uuid,(root->>'artifact_id')::uuid,root->>'sha256',
 (body->'learning'->'model'->>'artifact_id')::uuid,body->'learning'->'model'->>'sha256',(body->'learning'->>'version_id')::uuid,actor,session) IS DISTINCT FROM true
 OR photo.v26_render_descriptor_scope(p_tenant,project,(selected->>'asset_version_id')::uuid,(root->>'artifact_id')::uuid,root->>'sha256',
 (descriptor->>'artifact_id')::uuid,descriptor->>'sha256',analysis,ordinal,lock_sha,actor,session) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF kind='DescriptorShard' THEN RETURN ref=descriptor AND frame='null'::jsonb AND p_evidence->'frame_ordinal'='null'::jsonb;END IF;
 IF frame IS NULL OR frame='null'::jsonb THEN RETURN false;END IF;
 IF kind='WorkingImage' THEN
 RETURN ref=frame->'source' AND photo.v26_render_frame_source_scope(p_tenant,project,(selected->>'asset_version_id')::uuid,(root->>'artifact_id')::uuid,root->>'sha256',
 (descriptor->>'artifact_id')::uuid,descriptor->>'sha256',analysis,ordinal,lock_sha,(p_evidence->>'frame_ordinal')::integer,(frame->>'asset_version_id')::uuid,
 (frame->'source'->>'artifact_id')::uuid,frame->'source'->>'sha256',actor,session);
 ELSIF kind='FaceObservations' THEN
 RETURN photo.v26_render_frame_faces_scope(p_tenant,project,(selected->>'asset_version_id')::uuid,(root->>'artifact_id')::uuid,root->>'sha256',
 (descriptor->>'artifact_id')::uuid,descriptor->>'sha256',analysis,ordinal,lock_sha,(p_evidence->>'frame_ordinal')::integer,(frame->>'asset_version_id')::uuid,
 (frame->'source'->>'artifact_id')::uuid,frame->'source'->>'sha256',(ref->>'artifact_id')::uuid,ref->>'sha256',actor,session);
 ELSIF kind IN('WorkingPixels','WorkingAlpha') THEN
 RETURN photo.v26_render_frame_array_scope(p_tenant,project,(selected->>'asset_version_id')::uuid,(root->>'artifact_id')::uuid,root->>'sha256',
 (descriptor->>'artifact_id')::uuid,descriptor->>'sha256',analysis,ordinal,lock_sha,(p_evidence->>'frame_ordinal')::integer,(frame->>'asset_version_id')::uuid,
 (frame->'source'->>'artifact_id')::uuid,frame->'source'->>'sha256',(ref->>'artifact_id')::uuid,ref->>'sha256',kind,actor,session);
 END IF;
 RETURN false;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_descriptor_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_descriptor_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_descriptor_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,jsonb) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
