-- +goose Up
-- NOWAIT avoids waiting on authority rows while another path owns child/task.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v30_watermark_stage_source(p_expansion uuid,p_token uuid,p_slot integer) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;expected jsonb;raw text;pin text;body jsonb;item jsonb;
BEGIN
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);IF expected IS NULL THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT pin_jcs,pin_sha INTO raw,pin FROM photo.read_v30_watermark_pin(e.tenant_id,e.run_id,e.id,p_token,(expected->'preset'->>'preset_id')::uuid,expected->'preset'->>'preset_sha');
 IF NOT FOUND THEN RETURN false;END IF;body:=raw::jsonb;
 PERFORM parent.id FROM photo.tenants tenant JOIN photo.runs parent ON parent.tenant_id=tenant.id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=parent.tenant_id AND workflow.run_id=parent.id
 JOIN photo.projects project ON project.tenant_id=workflow.tenant_id AND project.id=workflow.project_id
 JOIN photo.graph_expansions expansion ON expansion.tenant_id=parent.tenant_id AND expansion.run_id=parent.id
 WHERE tenant.id=e.tenant_id AND parent.id=e.run_id AND expansion.id=e.id
 FOR SHARE OF tenant,parent,workflow,project,expansion NOWAIT;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version.id FROM photo.asset_versions version JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 JOIN photo.artifacts master ON master.tenant_id=version.tenant_id AND master.id=version.master_artifact_id
 WHERE version.tenant_id=e.tenant_id AND version.id=(body->'binding'->>'asset_version_id')::uuid AND asset.id=(body->>'asset_id')::uuid
 AND master.id=(body->'binding'->'source'->>'artifact_id')::uuid
 FOR SHARE OF version,asset,master NOWAIT;
 IF NOT FOUND THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(body->'arrays') ORDER BY value->'ref'->>'artifact_id' LOOP
  PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=e.tenant_id AND a.id=(item->'ref'->>'artifact_id')::uuid FOR SHARE NOWAIT;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 -- Repeat complete current readiness after all source locks, held until commit.
 RETURN EXISTS(SELECT 1 FROM photo.read_v30_watermark_pin(e.tenant_id,e.run_id,e.id,p_token,(expected->'preset'->>'preset_id')::uuid,expected->'preset'->>'preset_sha') fresh WHERE fresh.pin_jcs=raw AND fresh.pin_sha=pin);
EXCEPTION WHEN lock_not_available OR invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v30_watermark_stage_source(uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v30_watermark_stage_source(uuid,uuid,integer) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE definition text;needle text:='fresh:=photo.v30_watermark_stage_result(p_expansion,p_token,p_slot,p_operation);';
BEGIN
 SELECT pg_get_functiondef('photo.record_v30_watermark_stage_verification(uuid,uuid,integer,text,jsonb,text,text)'::regprocedure) INTO definition;
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark verification source lock patch mismatch';END IF;
 EXECUTE replace(definition,needle,'IF photo.lock_v30_watermark_stage_source(p_expansion,p_token,p_slot) IS DISTINCT FROM true THEN RETURN false;END IF;'||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
