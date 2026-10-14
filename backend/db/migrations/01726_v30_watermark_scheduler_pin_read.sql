-- +goose Up
-- Fenced scheduler reads immutable pin bytes, never caller-selected object keys.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_watermark_pin(p_tenant uuid,p_parent uuid,p_expansion uuid,p_token uuid,p_preset uuid,p_sha text)
RETURNS TABLE(pin_jcs text,pin_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;proof jsonb;cfg text;entry jsonb;frozen jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_token IS NULL OR p_preset IS NULL THEN RETURN;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion AND run_id=p_parent AND kind='v2.delivery/1'
 AND state='open' AND expansion_token=p_token AND expansion_lease_until>clock_timestamp();
 IF NOT FOUND OR photo.v30_delivery_expansion_ready(e.id) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent;
 SELECT d.v30_delivery_proof INTO proof FROM photo.backend_documents d JOIN photo.tasks t
 ON t.tenant_id=d.tenant_id AND t.id=d.owner_id AND t.params_artifact_id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_parent AND d.owner_id=e.parent_task_id AND d.purpose='task_params' AND d.state='ready';
 IF proof IS NULL THEN RETURN;END IF;
 frozen:=(proof->>'frozen_input_jcs')::jsonb;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(frozen->'presets') WHERE value->>'preset_id'=p_preset::text AND value->>'preset_sha'=p_sha) THEN RETURN;END IF;
 SELECT value->>'config_jcs' INTO cfg FROM jsonb_array_elements(proof->'configs_jcs') WHERE value->>'preset_id'=p_preset::text;
 IF photo.v30_watermark_pinned_source_ready(p_tenant,p_parent,r.created_by,(proof->>'auth_session_id')::uuid,p_preset,p_sha,cfg) IS DISTINCT FROM true THEN RETURN;END IF;
 entry:=r.v30_watermark_sources->p_preset::text;
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN;END IF;
 RETURN QUERY SELECT entry->>'pin_jcs',entry->>'pin_sha256';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.read_v30_watermark_pin(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_watermark_pin(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_watermark_pin(uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
