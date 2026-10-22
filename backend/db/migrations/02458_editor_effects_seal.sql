-- +goose Up
-- A sealed base is a new identity result over the processed image. Original
-- recipe/style/trace provenance remains distinct and cannot be replaced by a
-- final image hash. Missing prepared seal provenance is unavailable.
ALTER TABLE photo.editor_effect_builds
 ADD COLUMN IF NOT EXISTS executed_style_artifact_id uuid,
 ADD COLUMN IF NOT EXISTS executed_style_sha text CHECK(executed_style_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN seal_trace_sha text CHECK(seal_trace_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN seal_run_id uuid,
 ADD COLUMN seal_task_id uuid,
 ADD COLUMN seal_params_artifact_id uuid,
 ADD COLUMN seal_params_sha text CHECK(seal_params_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN seal_generated_mask jsonb,
 ADD COLUMN seal_generative_requested boolean,
 ADD COLUMN seal_inherited_review boolean,
 ADD COLUMN seal_receipt_artifact_id uuid,
 ADD COLUMN seal_receipt_sha text CHECK(seal_receipt_sha ~ '^[0-9a-f]{64}$');
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends)
 VALUES('editor.effects_seal','1','cpu','cpu.editor_effects_seal',NULL,300,'["cpu"]'::jsonb);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('editor.effects_seal','1','changed_pixels','["application/octet-stream"]'::jsonb,800008192,1),
 ('editor.effects_seal','1','changed_mask','["application/json"]'::jsonb,2097152,1),
 ('editor.effects_seal','1','generated_mask','["application/json"]'::jsonb,2097152,1),
 ('editor.effects_seal','1','image_report','["application/json"]'::jsonb,2097152,1),
 ('editor.effects_seal','1','image_result','["application/json"]'::jsonb,2097152,1),
 ('editor.effects_seal','1','executed_base_receipt','["application/json"]'::jsonb,2097152,1);
-- +goose StatementBegin
CREATE FUNCTION photo.editor_effect_seal_worker_ready(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;b photo.editor_effect_builds%ROWTYPE;
 slot photo.resource_slots%ROWTYPE;body jsonb;origin photo.runs%ROWTYPE;producer photo.tasks%ROWTYPE;quality photo.tasks%ROWTYPE;
BEGIN
 IF p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_json IS NULL
 OR octet_length(p_json) NOT BETWEEN 2 AND 1048576 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_digest,'') !~ '^[0-9a-f]{64}$' OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha
 OR NOT EXISTS(SELECT FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker AND db_principal=session_user AND enabled) THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF r.id IS NULL OR r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed OR t.id IS NULL
 OR t.state<>'running' OR t.cancel_requested OR t.operation<>'editor.effects_seal' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence OR t.worker_id IS DISTINCT FROM p_worker
 OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
 OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp() OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' THEN RETURN false;END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF slot.id IS NULL OR slot.resource_class<>'cpu' OR slot.fencing_token IS DISTINCT FROM p_fence
 OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp()
 OR photo.worker_authorized(p_tenant,p_worker,'cpu',slot.node_id,t.operation,'cpu',NULL) IS DISTINCT FROM true THEN RETURN false;END IF;
 body:=p_json::jsonb;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=(body->>'build_run_id')::uuid FOR SHARE;
 IF b.run_id IS NULL OR b.state<>'sealing' OR b.seal_run_id IS DISTINCT FROM p_run OR b.seal_task_id IS DISTINCT FROM p_task
 OR b.seal_params_artifact_id IS DISTINCT FROM p_params OR b.seal_params_sha IS DISTINCT FROM p_sha
 OR b.executed_style_artifact_id IS NULL OR b.seal_trace_sha IS NULL OR b.seal_generative_requested IS NULL
 OR b.seal_inherited_review IS NULL OR b.processed_source_artifact_id IS NULL OR b.processed_source_canvas_hash IS NULL
 OR b.quality_artifact_id IS NULL OR b.producer_result_id IS NULL OR b.quality_result_id IS NULL
 OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR body->'original_recipe' IS DISTINCT FROM jsonb_build_object('artifact_id',b.frozen_recipe_artifact_id,'sha256',b.frozen_recipe_sha)
 OR body->'original_source' IS DISTINCT FROM jsonb_build_object('asset_version_id',b.asset_version_id,'source',jsonb_build_object('artifact_id',b.source_artifact_id,'sha256',b.source_sha),'canvas_hash',b.source_canvas_hash)
 OR body->'processed_source' IS DISTINCT FROM jsonb_build_object('asset_version_id',b.asset_version_id,'source',jsonb_build_object('artifact_id',b.processed_source_artifact_id,'sha256',b.processed_source_sha),'canvas_hash',b.processed_source_canvas_hash)
 OR body->'quality' IS DISTINCT FROM jsonb_build_object('artifact_id',b.quality_artifact_id,'sha256',b.quality_sha)
 OR body->'executed_style' IS DISTINCT FROM jsonb_build_object('artifact_id',b.executed_style_artifact_id,'sha256',b.executed_style_sha)
 OR body->>'compilation_sha' IS DISTINCT FROM b.compilation_sha OR body->>'trace_sha' IS DISTINCT FROM b.seal_trace_sha
 OR body->'generative_requested' IS DISTINCT FROM to_jsonb(b.seal_generative_requested)
 OR body->'inherited_review' IS DISTINCT FROM to_jsonb(b.seal_inherited_review)
 OR body->'generated' IS DISTINCT FROM coalesce(b.seal_generated_mask,'null'::jsonb)
 OR b.seal_generative_requested IS DISTINCT FROM (b.seal_generated_mask IS NOT NULL) THEN RETURN false;END IF;
 SELECT * INTO origin FROM photo.runs WHERE tenant_id=p_tenant AND id=b.run_id FOR SHARE;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=b.run_id AND id=b.producer_task_id FOR SHARE;
 SELECT * INTO quality FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=b.run_id AND id=b.quality_task_id FOR SHARE;
 IF origin.id IS NULL OR origin.cancel_requested OR producer.id IS NULL OR quality.id IS NULL
 OR producer.state<>'succeeded' OR producer.cancel_requested OR producer.canonical_result_id IS DISTINCT FROM b.producer_result_id
 OR producer.attempt IS DISTINCT FROM b.producer_attempt OR producer.fencing_token IS DISTINCT FROM b.producer_fence
 OR quality.state<>'succeeded' OR quality.cancel_requested OR quality.canonical_result_id IS DISTINCT FROM b.quality_result_id
 OR quality.operation<>'quality.check'
 OR photo.v2_editor_authorized(p_tenant,b.project_id,b.actor_id,b.session_id) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.task_results result ON result.tenant_id=a.tenant_id AND result.id=b.producer_result_id
 WHERE a.tenant_id=p_tenant AND a.id=b.processed_source_artifact_id AND a.sha256=b.processed_source_sha AND a.state='ready'
 AND a.created_by_task_id=b.producer_task_id AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)))
 OR NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.task_results result ON result.tenant_id=a.tenant_id AND result.id=b.quality_result_id
 WHERE a.tenant_id=p_tenant AND a.id=b.quality_artifact_id AND a.sha256=b.quality_sha AND a.state='ready' AND a.output_role='quality_report'
 AND a.created_by_task_id=b.quality_task_id AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_task
 AND d.purpose='task_params' AND d.artifact_id=p_params AND d.sha256=p_sha AND d.bytes=octet_length(p_json) AND d.state='ready')
 OR EXISTS(SELECT FROM jsonb_array_elements(t.inputs) input LEFT JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(input->>'artifact_id')::uuid AND a.sha256=input->>'sha256'
 WHERE a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR a.expires_at<=clock_timestamp())
 OR NOT EXISTS(SELECT FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
 AND q.state='reserved' AND q.expires_at>clock_timestamp() AND q.bytes>=(t.execution_profile#>>'{resource_estimate,output_bytes}')::bigint) THEN RETURN false;END IF;
 RETURN t.lease_until>clock_timestamp() AND slot.lease_until>clock_timestamp() AND t.deadline>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
