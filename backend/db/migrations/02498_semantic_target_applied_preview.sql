-- +goose Up
-- Target-specific media is the exact presentation copied by the adopted shown task.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_applied_preview_object(p_tenant uuid,p_target uuid,p_actor uuid,p_session uuid,p_kind text,p_params text,p_presentation text)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proof record;t photo.tasks%ROWTYPE;a photo.artifacts%ROWTYPE;body jsonb;presentation jsonb;ref jsonb;preview photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;expected text;presentation_contract text;evidence_contract text;
BEGIN
 IF p_kind IS NULL OR p_kind NOT IN('params','presentation','source_image','after') THEN RETURN;END IF;
 SELECT * INTO proof FROM photo.semantic_shown_applied_image(p_tenant,p_target,p_actor,p_session);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=proof.task_id AND canonical_result_id=proof.result_id AND run_id=proof.child_run_id AND state='succeeded' AND operation_version='1' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT x.* INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id=t.params_artifact_id AND x.state='ready' AND x.verified_at IS NOT NULL AND x.media_type='application/json' AND x.bytes BETWEEN 2 AND 65536 AND x.object_version NOT IN('','null') AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 IF NOT EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='Params') THEN RETURN;END IF;
 IF p_kind='params' THEN RETURN QUERY SELECT 'Params'::text,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type;RETURN;END IF;
 IF p_params IS NULL OR octet_length(p_params)<>a.bytes OR encode(sha256(convert_to(p_params,'UTF8')),'hex')<>a.sha256 THEN RETURN;END IF;
 body:=p_params::jsonb;
 IF body->>'target_id' IS DISTINCT FROM p_target::text OR body->>'tenant_id' IS DISTINCT FROM p_tenant::text OR body->>'child_run_id' IS DISTINCT FROM proof.child_run_id::text THEN RETURN;END IF;
 expected:=CASE t.operation WHEN 'semantic.adopt_shown_exposure' THEN 'semantic.preview_local_exposure' WHEN 'semantic.adopt_shown_removal' THEN 'semantic.preview_remove_object' WHEN 'semantic.adopt_shown_background_cleanup' THEN 'semantic.preview_background_cleanup' WHEN 'semantic.adopt_shown_retouch_region' THEN 'semantic.preview_retouch_region' END;
 presentation_contract:=CASE expected WHEN 'semantic.preview_local_exposure' THEN 'ExposurePreviewPresentation' WHEN 'semantic.preview_remove_object' THEN 'RemovalPreviewPresentation' WHEN 'semantic.preview_background_cleanup' THEN 'BackgroundCleanupPreviewPresentation' WHEN 'semantic.preview_retouch_region' THEN 'RetouchPreviewPresentation' END;
 evidence_contract:=replace(presentation_contract,'Presentation','NumericalEvidence');
 IF expected IS NULL OR NOT EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(body->'working_image'->>'artifact_id')::uuid AND i.sha256=body->'working_image'->>'sha256' AND i.contract='WorkingImage')
 OR NOT EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(body->'pixels'->>'artifact_id')::uuid AND i.sha256=body->'pixels'->>'sha256' AND i.contract='NpyArray')
 OR NOT EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(body->'numerical_evidence'->>'artifact_id')::uuid AND i.sha256=body->'numerical_evidence'->>'sha256' AND i.contract=evidence_contract) THEN RETURN;END IF;
 SELECT * INTO preview FROM photo.tasks WHERE tenant_id=p_tenant AND id=(body->>'preview_task_id')::uuid AND run_id=(body->>'preview_run_id')::uuid AND id=photo.semantic_uuid_v5(run_id,expected||':'||p_target::text) AND operation=expected AND operation_version='1' AND state='succeeded' AND NOT cancel_requested AND attempt=(body->>'preview_attempt')::integer AND fencing_token=(body->>'preview_fence')::bigint FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=preview.canonical_result_id AND id=(body->>'preview_result_id')::uuid AND task_id=preview.id AND attempt=preview.attempt AND fencing_token=preview.fencing_token AND outputs=jsonb_build_array(body->'presentation') FOR SHARE;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(preview.id,result.id)) OR NOT EXISTS(SELECT FROM photo.runs WHERE tenant_id=p_tenant AND id=preview.run_id AND status='completed' AND graph_sealed AND NOT cancel_requested) THEN RETURN;END IF;
 ref:=body->'presentation';
 IF NOT EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(ref->>'artifact_id')::uuid AND i.sha256=ref->>'sha256' AND i.contract=presentation_contract) THEN RETURN;END IF;
 SELECT x.* INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id=(ref->>'artifact_id')::uuid AND x.sha256=ref->>'sha256' AND x.created_by_task_id=preview.id AND x.produced_attempt=preview.attempt AND x.produced_fence=preview.fencing_token AND x.output_role='preview_presentation' AND x.state='ready' AND x.verified_at IS NOT NULL AND x.media_type='application/json' AND x.bytes BETWEEN 2 AND 1048576 AND x.object_version NOT IN('','null') AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 IF p_kind='presentation' THEN RETURN QUERY SELECT 'Presentation'::text,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type;RETURN;END IF;
 IF p_presentation IS NULL OR octet_length(p_presentation)<>a.bytes OR encode(sha256(convert_to(p_presentation,'UTF8')),'hex')<>a.sha256 THEN RETURN;END IF;
 presentation:=p_presentation::jsonb;
 IF presentation->'working_image' IS DISTINCT FROM body->'working_image' OR presentation->'pixels' IS DISTINCT FROM body->'pixels' OR presentation->'numerical_evidence' IS DISTINCT FROM body->'numerical_evidence' OR presentation->'params'->>'target_id' IS DISTINCT FROM p_target::text THEN RETURN;END IF;
 IF p_kind='source_image' THEN
 ref:=body->'working_image';
 RETURN QUERY SELECT 'WorkingImage'::text,x.id,x.sha256,x.bytes,x.bucket,x.object_key,x.object_version,x.media_type FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id=(ref->>'artifact_id')::uuid AND x.sha256=ref->>'sha256' AND x.created_by_task_id=preview.id AND x.produced_attempt=preview.attempt AND x.produced_fence=preview.fencing_token AND x.output_role='working_image' AND x.media_type='application/json' AND x.state='ready' AND x.verified_at IS NOT NULL AND x.bytes BETWEEN 2 AND 1048576 AND x.object_version NOT IN('','null') AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp());RETURN;
 END IF;
 ref:=presentation->'after';
 RETURN QUERY SELECT 'PNG'::text,x.id,x.sha256,x.bytes,x.bucket,x.object_key,x.object_version,x.media_type FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id=(ref->>'artifact_id')::uuid AND x.sha256=ref->>'sha256' AND x.created_by_task_id=preview.id AND x.produced_attempt=preview.attempt AND x.produced_fence=preview.fencing_token AND x.output_role='after' AND x.media_type='image/png' AND x.state='ready' AND x.verified_at IS NOT NULL AND x.bytes BETWEEN 33 AND 33882112 AND x.object_version NOT IN('','null') AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) AND EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=x.id AND i.sha256=x.sha256 AND i.contract='EncodedImage');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_shown_applied_preview_object(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_applied_preview_object(uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_shown_applied_preview_object(uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
