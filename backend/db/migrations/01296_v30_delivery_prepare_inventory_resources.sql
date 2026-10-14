-- +goose Up
-- The direct transport envelope has two roots; resource accounting must use
-- the complete registered inventory, including Params and binary dependencies.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_prepare_resource_estimate_ready(p_estimate jsonb,p_document_bytes bigint,p_input_count bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_document_bytes IS NULL OR p_document_bytes NOT BETWEEN 1 AND 27262976
 OR p_input_count IS NULL OR p_input_count NOT BETWEEN 1 AND 1000000
 OR jsonb_typeof(p_estimate) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_estimate->'ram_bytes') IS DISTINCT FROM 'number'
 OR jsonb_typeof(p_estimate->'scratch_bytes') IS DISTINCT FROM 'number'
 OR (p_estimate->>'ram_bytes')::numeric<536870912+32::numeric*p_document_bytes+1024::numeric*p_input_count
 OR (p_estimate->>'scratch_bytes')::numeric<33554432+p_document_bytes+1024::numeric*p_input_count
 OR (p_estimate->>'ram_bytes')::numeric>9007199254740991
 OR (p_estimate->>'scratch_bytes')::numeric>9007199254740991
 OR trunc((p_estimate->>'ram_bytes')::numeric)<>(p_estimate->>'ram_bytes')::numeric
 OR trunc((p_estimate->>'scratch_bytes')::numeric)<>(p_estimate->>'scratch_bytes')::numeric
 OR p_estimate->'vram_bytes' IS DISTINCT FROM '0'::jsonb
 OR p_estimate->'pixel_count' IS DISTINCT FROM '0'::jsonb
 OR p_estimate->'output_bytes' IS DISTINCT FROM '8388608'::jsonb THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_prepare_resource_estimate_ready(jsonb,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_prepare_resource_estimate_ready(jsonb,bigint,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER FUNCTION photo.v30_delivery_prepare_source_ready(uuid,uuid,uuid,uuid,text,text)
 RENAME TO v30_delivery_prepare_source_ready_before_inventory_resources;
REVOKE ALL ON FUNCTION photo.v30_delivery_prepare_source_ready_before_inventory_resources(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

CREATE FUNCTION photo.v30_delivery_prepare_source_ready(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_sha text,p_json text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;proof jsonb;cfg jsonb;estimate jsonb;document_bytes bigint;input_count bigint;
BEGIN
 IF photo.v30_delivery_prepare_source_ready_before_inventory_resources(p_tenant,p_run,p_task,p_params,p_sha,p_json) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF t.id IS NULL OR t.operation IS DISTINCT FROM 'v2.delivery.prepare' OR t.params_artifact_id IS DISTINCT FROM p_params THEN RETURN false;END IF;
 SELECT v30_delivery_proof INTO proof FROM photo.backend_documents
 WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_task AND purpose='task_params'
 AND artifact_id=p_params AND sha256=p_sha AND state='ready' FOR SHARE;
 IF proof IS NULL THEN RETURN false;END IF;
 document_bytes:=octet_length(p_json)::bigint+octet_length(proof->>'frozen_input_jcs')::bigint+octet_length(proof->>'snapshot_jcs')::bigint;
 FOR cfg IN SELECT value FROM jsonb_array_elements(proof->'configs_jcs') LOOP
  document_bytes:=document_bytes+octet_length(cfg->>'config_jcs')::bigint;
 END LOOP;
 SELECT count(*) INTO input_count FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract NOT IN('InputClosureChunk','InputClosureManifest');
 estimate:=t.execution_profile->'resource_estimate';
 IF photo.v30_delivery_prepare_resource_estimate_ready(estimate,document_bytes,input_count) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_prepare_source_ready(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_prepare_source_ready(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
