-- +goose Up
-- Read-only exact draft recovery. No missing-state repair or admission.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.read_v29_replacement_detector_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_descriptor text,p_original text,p_snapshot uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d jsonb;config jsonb;detector_run uuid;old photo.runs%ROWTYPE;
BEGIN
 IF photo.lock_v29_replacement_detector_preparation(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,p_descriptor,p_original,p_snapshot) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 d:=p_descriptor::jsonb;detector_run:=(d->>'run_id')::uuid;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(d->'base'->'source'->>'artifact_id')::uuid
 AND a.sha256=d->'base'->'source'->>'sha256' AND a.bytes=octet_length(d->>'source_jcs')
 AND a.media_type='application/json' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 config:=jsonb_build_object('project_id',p_project,'v29_negative_replacement',
 jsonb_build_object('version','v29_perspective_replacement_detection_v1','project_id',p_project,'review_id',p_review,
 'decision_hash',p_hash,'original_task_id',d->'replacement'->>'original_task_id','original_run_id',d->'replacement'->>'original_run_id'),
 'v29_perspective_replacement_detection_jcs',p_descriptor);
 SELECT * INTO old FROM photo.runs WHERE id=detector_run FOR UPDATE;
 IF NOT FOUND OR old.tenant_id IS DISTINCT FROM p_tenant OR old.kind IS DISTINCT FROM 'v2'
 OR old.status IS DISTINCT FROM 'draft' OR old.created_by IS DISTINCT FROM p_actor
 OR old.input_snapshot_id IS DISTINCT FROM p_snapshot OR old.failure_policy IS DISTINCT FROM 'fail_fast'
 OR old.graph_sealed OR old.cancel_requested OR old.config_snapshot IS DISTINCT FROM config
 OR old.config_hash IS DISTINCT FROM encode(sha256(convert_to(config::text,'UTF8')),'hex')
 OR EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=detector_run
  AND (t.id IS DISTINCT FROM (d->>'task_id')::uuid OR t.state IS DISTINCT FROM 'blocked'
   OR t.cancel_requested OR t.canonical_result_id IS NOT NULL OR t.operation IS DISTINCT FROM 'optical.perspective_lines'
   OR t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
   OR t.node_key IS DISTINCT FROM 'optical.perspective_lines'
   OR t.params_artifact_id IS DISTINCT FROM photo.v30_uuid5((d->>'task_id')::uuid,'params')
   OR (t.execution_profile IS DISTINCT FROM (d->>'profile_jcs')::jsonb AND (old.v29_replacement_detector_seal_receipt IS NULL OR photo.read_v29_replacement_detector_seal_receipt(p_tenant,detector_run,p_descriptor) IS NULL)))) THEN RETURN NULL;END IF;
 IF old.v29_replacement_detector_seal_receipt IS NOT NULL AND photo.read_v29_replacement_detector_seal_receipt(p_tenant,detector_run,p_descriptor) IS NULL THEN RETURN NULL;END IF;
 RETURN old.config_snapshot->>'v29_perspective_replacement_detection_jcs';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.seal_v29_replacement_detector_transport(p_tenant uuid,p_run uuid,p_proof jsonb,p_closure jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE descriptor jsonb;receipt jsonb;
BEGIN
 IF p_proof IS NULL OR p_closure IS NULL OR p_witness IS NULL OR pg_column_size(p_proof)>4194304
 OR pg_column_size(p_closure)>262144 OR pg_column_size(p_witness)>131072 THEN RETURN false;END IF;
 descriptor:=(p_proof->>'descriptor_jcs')::jsonb;
 -- Fresh draft/source identity must pass even when acknowledging an old seal.
 IF photo.v29_replacement_detector_params_document_scope(p_tenant,p_run,(descriptor->>'task_id')::uuid,'task_params',
 encode(sha256(convert_to(descriptor->>'params_jcs','UTF8')),'hex'),octet_length(descriptor->>'params_jcs'),p_proof) IS DISTINCT FROM true THEN RETURN false;END IF;
 receipt:=photo.read_v29_replacement_detector_seal_receipt(p_tenant,p_run,p_proof->>'descriptor_jcs');
 IF receipt IS NOT NULL THEN
  RETURN receipt=jsonb_build_object('schema_version',1,'descriptor_jcs',p_proof->>'descriptor_jcs','closure',p_closure,'witness',p_witness);
 END IF;
 IF photo.seal_v29_replacement_detector_transport_first(p_tenant,p_run,p_proof,p_closure,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.runs SET v29_replacement_detector_seal_receipt=jsonb_build_object('schema_version',1,'descriptor_jcs',p_proof->>'descriptor_jcs','closure',p_closure,'witness',p_witness)
 WHERE tenant_id=p_tenant AND id=p_run AND v29_replacement_detector_seal_receipt IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'REPLACEMENT_DETECTOR_SEAL_RECEIPT_CONFLICT' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
