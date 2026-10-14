-- +goose Up
-- Watermark authority must still hold when encoded output becomes deliverable.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_encode_completion_ready(p_tenant uuid,p_task uuid,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;parent photo.runs%ROWTYPE;e photo.graph_expansions%ROWTYPE;
 request jsonb;cfg jsonb;proof jsonb;mark photo.tasks%ROWTYPE;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='export.encode';
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 request:=r.config_snapshot->'v30_delivery_export';cfg:=(request->>'config_jcs')::jsonb;
 IF request IS NULL OR cfg IS NULL THEN RETURN false;END IF;
 IF cfg->'watermark' IS NULL THEN RETURN true;END IF;
 IF jsonb_typeof(cfg->'watermark') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=(request->>'parent_run_id')::uuid;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=parent.id AND kind='v2.delivery/1' AND state='open';
 IF NOT FOUND OR parent.cancel_requested OR r.cancel_requested THEN RETURN false;END IF;
 SELECT d.v30_delivery_proof INTO proof FROM photo.backend_documents d JOIN photo.tasks prepare
 ON prepare.tenant_id=d.tenant_id AND prepare.id=d.owner_id AND prepare.params_artifact_id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=parent.id AND d.owner_id=e.parent_task_id AND d.purpose='task_params' AND d.state='ready';
 IF proof IS NULL OR photo.v30_watermark_pinned_source_ready(p_tenant,parent.id,parent.created_by,(proof->>'auth_session_id')::uuid,
 (request->'preset'->>'preset_id')::uuid,request->'preset'->>'preset_sha',request->>'config_jcs') IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO mark FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id
 AND id=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'derivative.watermark:0')
 AND operation='derivative.watermark' AND operation_version='1' AND state='succeeded' AND NOT cancel_requested;
 IF NOT FOUND OR mark.canonical_result_id IS NULL THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(mark.id,mark.canonical_result_id)) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-watermark-stage-verified'
 AND event_id=photo.v2_adoption_event_id(mark.id,mark.canonical_result_id)) THEN RETURN false;END IF;
 IF p_params IS NOT NULL AND octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results z CROSS JOIN LATERAL jsonb_array_elements(z.outputs) output
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256'
 JOIN photo.v2_task_inputs i ON i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='WorkingImage'
 WHERE z.tenant_id=p_tenant AND z.task_id=mark.id AND z.id=mark.canonical_result_id AND z.attempt=mark.attempt AND z.fencing_token=mark.fencing_token
 AND a.output_role='working_image' AND (p_params IS NULL OR (a.id=(p_params::jsonb->'image'->>'artifact_id')::uuid AND a.sha256=p_params::jsonb->'image'->>'sha256'))) THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_watermark_encode_completion_ready(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_encode_completion_ready(uuid,uuid,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;

DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_delivery_encode_context(uuid,uuid)'::regprocedure);
 needle:='request:=r.config_snapshot->''v30_delivery_export'';';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_ENCODE_CONTEXT_DEFINITION_DRIFT';END IF;
 EXECUTE replace(definition,needle,needle||' IF photo.v30_watermark_encode_completion_ready(p_tenant,p_task,NULL) IS DISTINCT FROM true THEN RETURN;END IF;');
 definition:=pg_get_functiondef('photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)'::regprocedure);
 needle:='IF EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer=''v2-adopt'' AND event_id=marker) THEN';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_ENCODE_ADOPTION_DEFINITION_DRIFT';END IF;
 EXECUTE replace(definition,needle,'IF photo.v30_watermark_encode_completion_ready(p_tenant,p_task,p_params) IS DISTINCT FROM true THEN RETURN false;END IF; '||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
