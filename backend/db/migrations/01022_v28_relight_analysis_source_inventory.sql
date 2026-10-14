-- +goose Up
-- Exact child occurrence in SHA-bound original accepted WorkingImage.
-- Independent original-byte shape/content checks remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v28_relight_analysis_source_child(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,
 p_base jsonb,p_source text,p_child uuid,p_sha text,p_role text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE image jsonb;ref jsonb;step integer;item photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_source IS NULL OR octet_length(p_source) NOT BETWEEN 2 AND 1048576
 OR encode(sha256(convert_to(p_source,'UTF8')),'hex') IS DISTINCT FROM p_base->'source'->>'sha256'
 OR photo.lock_v28_relight_source(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true
 OR p_child IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 image:=p_source::jsonb;
 IF p_role='WorkingPixels' THEN ref:=image->'pixels';
 ELSIF p_role='WorkingAlpha' THEN ref:=image->'alpha';
 ELSIF p_role ~ '^InverseField/(0|[1-9][0-9]{0,2})$' THEN
 step:=split_part(p_role,'/',2)::integer;
 ref:=image->'transform_chain'->'steps'->step->'inverse_field';
 ELSE RETURN false;END IF;
 IF ref IS NULL OR ref->>'artifact_id' IS DISTINCT FROM p_child::text OR ref->>'sha256' IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 SELECT * INTO item FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_child AND sha256=p_sha FOR SHARE;
 IF NOT FOUND OR item.state IS DISTINCT FROM 'ready' OR item.verified_at IS NULL OR item.bytes<=0
 OR (item.media_type IS DISTINCT FROM 'application/x-npy' AND NOT(p_role IN('WorkingPixels','WorkingAlpha') AND coalesce(item.media_type,'')='application/octet-stream')) OR item.object_version IS NULL OR item.object_version IN('','null')
 OR (item.expires_at IS NOT NULL AND item.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
 IF p_role='WorkingPixels' AND coalesce(item.output_role,'') NOT IN('working_rgb','pixels') THEN RETURN false;END IF;
 IF p_role='WorkingAlpha' AND item.output_role IS DISTINCT FROM 'alpha' THEN RETURN false;END IF;
 IF p_role LIKE 'InverseField/%' AND item.output_role IS DISTINCT FROM 'warp_field' THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v28_relight_analysis_source_child(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_relight_analysis_source_child(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v28_relight_analysis_source_child(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v28_relight_analysis_source_inventory(p_tenant uuid,p_plan jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb:=p_plan->'config';image jsonb;source text:=p_plan->>'source_jcs';refs jsonb:='[]'::jsonb;edge jsonb;step jsonb;position integer:=0;actual jsonb;expected jsonb;
BEGIN
 IF source IS NULL OR octet_length(source) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 image:=source::jsonb;
 refs:=refs||jsonb_build_array(jsonb_build_object('role','WorkingPixels','ref',image->'pixels'));
 IF image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb THEN refs:=refs||jsonb_build_array(jsonb_build_object('role','WorkingAlpha','ref',image->'alpha'));END IF;
 FOR step IN SELECT value FROM jsonb_array_elements(image->'transform_chain'->'steps') LOOP
 IF step->>'kind'='dense_warp' THEN refs:=refs||jsonb_build_array(jsonb_build_object('role','InverseField/'||position,'ref',step->'inverse_field'));END IF;
 position:=position+1;
 END LOOP;
 IF jsonb_array_length(refs) NOT BETWEEN 1 AND 61 THEN RETURN false;END IF;
 FOR edge IN SELECT value FROM jsonb_array_elements(refs) LOOP
 IF photo.lock_v28_relight_analysis_source_child(p_tenant,(request->>'project_id')::uuid,(request->>'snapshot_id')::uuid,
 (request->>'actor_id')::uuid,(request->>'auth_session_id')::uuid,request->'base',source,(edge->'ref'->>'artifact_id')::uuid,edge->'ref'->>'sha256',edge->>'role') IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 SELECT jsonb_agg(value ORDER BY value->'ref'->>'artifact_id') INTO actual FROM jsonb_array_elements(p_plan->'inputs') WHERE value->>'contract'='NpyArray';
 SELECT jsonb_agg(value ORDER BY value->'ref'->>'artifact_id') INTO expected FROM (
 SELECT DISTINCT jsonb_build_object('contract','NpyArray','ref',value->'ref') value FROM jsonb_array_elements(refs)) unique_refs;
 RETURN actual IS NOT NULL AND actual=expected;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v28_relight_analysis_source_inventory(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_relight_analysis_source_inventory(uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v28_relight_analysis_params_document_scope(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_sha text,p_bytes bigint,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE config jsonb;plan jsonb;request jsonb;body text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_purpose IS DISTINCT FROM 'task_params' OR p_bytes NOT BETWEEN 2 AND 65536
 OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object' OR pg_column_size(p_proof)>524288
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_proof) k) IS DISTINCT FROM ARRAY['params_jcs','plan']::text[] THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT config_snapshot INTO config FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR UPDATE;
 plan:=config->'v28_analysis_plan';request:=plan->'config';body:=p_proof->>'params_jcs';
 IF plan IS NULL OR plan IS DISTINCT FROM p_proof->'plan' OR plan->>'run_id' IS DISTINCT FROM p_run::text
 OR plan->>'task_id' IS DISTINCT FROM p_owner::text OR plan->>'params_sha' IS DISTINCT FROM p_sha
 OR body IS NULL OR octet_length(body) IS DISTINCT FROM p_bytes
 OR encode(sha256(convert_to(body,'UTF8')),'hex') IS DISTINCT FROM p_sha OR body::jsonb IS DISTINCT FROM plan->'params' THEN RETURN false;END IF;
 IF photo.lock_v28_relight_analysis_source_inventory(p_tenant,plan) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.save_v28_relight_analysis_draft(p_tenant,(request->>'actor_id')::uuid,(request->>'auth_session_id')::uuid,
 plan::text,jsonb_build_object('project_id',request->>'project_id','v28_analysis_plan',plan)::text);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_relight_analysis_params_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_analysis_params_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_analysis_params_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
