-- +goose Up
-- Generic registry/transport sealing cannot bypass donor-specific authority.
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_v28_donor;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_v28_donor(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND v28_donor_descriptor_jcs IS NOT NULL)
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='portrait.open_eyes') THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v28_donor(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
CREATE FUNCTION photo.seal_v28_donor_graph(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_manual jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;task_id uuid;origin record;vector_id uuid;vector_sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_manual) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_manual) k) IS DISTINCT FROM ARRAY['guard_pixel','original_params','root','target_pixel','vectors']::text[]
 OR photo.v25_layer_ref_shape(p_manual->'root') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(p_manual->'original_params') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(p_manual->'target_pixel') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(p_manual->'guard_pixel') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF p_manual->'vectors' IS DISTINCT FROM 'null'::jsonb THEN
 IF photo.v25_layer_ref_shape(p_manual->'vectors') IS DISTINCT FROM true THEN RETURN false;END IF;
 vector_id:=(p_manual->'vectors'->>'artifact_id')::uuid;vector_sha:=p_manual->'vectors'->>'sha256';END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR r.v28_donor_descriptor_jcs IS NULL THEN RETURN false;END IF;
 task_id:=(r.v28_donor_descriptor_jcs::jsonb->'task'->>'id')::uuid;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR photo.read_v28_donor_registered_transport(p_tenant,p_run,task_id,p_actor,p_session) IS NULL THEN RETURN false;END IF;
 SELECT * INTO origin FROM photo.v28_donor_draft_manual_origin(p_tenant,p_run,task_id,
 (p_manual->'target_pixel'->>'artifact_id')::uuid,p_manual->'target_pixel'->>'sha256',
 (p_manual->'guard_pixel'->>'artifact_id')::uuid,p_manual->'guard_pixel'->>'sha256',vector_id,vector_sha);
 IF NOT FOUND OR origin.root_id::text IS DISTINCT FROM p_manual->'root'->>'artifact_id'
 OR origin.root_sha IS DISTINCT FROM p_manual->'root'->>'sha256'
 OR origin.params_id::text IS DISTINCT FROM p_manual->'original_params'->>'artifact_id'
 OR origin.params_sha IS DISTINCT FROM p_manual->'original_params'->>'sha256' THEN RETURN false;END IF;
 -- Existing registry/output-policy/quota/DAG/activation/event/outbox gates
 -- execute under the same locks. Any exception rolls the caller transaction back.
 RETURN photo.seal_v2_graph_pre_v28_donor(p_tenant,p_run);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.seal_v28_donor_graph(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v28_donor_graph(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v28_donor_graph(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
