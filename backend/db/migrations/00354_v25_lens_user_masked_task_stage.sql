-- +goose Up
-- Adopted lens proposal and complete user-mask proof under the same run lock.
-- No runnable activation or worker qualification is implied.
-- +goose StatementBegin
CREATE FUNCTION photo.stage_v25_lens_user_masked_composition_task(p_tenant uuid,p_run uuid,p_task uuid,p_ordinal integer,
 p_params uuid,p_sha text,p_digest text,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; node jsonb; params jsonb;
BEGIN
 IF p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR w.id IS NULL OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 node:=r.config_snapshot->'v25_composition'->'nodes'->p_ordinal;
 IF node->>'task_id' IS DISTINCT FROM p_task::text THEN RETURN false;END IF;
 BEGIN params:=(node->>'params_jcs')::jsonb;EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF NOT photo.v25_layer_original_user_mask_set_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
  (r.config_snapshot->'v25_composition'->>'auth_session_id')::uuid,params,
  (node->>'width')::bigint,(node->>'height')::bigint) THEN RETURN false;END IF;
 IF NOT photo.v25_layer_lens_proposal_scope(p_tenant,w.project_id,
  (params->'base'->>'asset_version_id')::uuid,r.created_by,
  (r.config_snapshot->'v25_composition'->>'auth_session_id')::uuid,
  (params->'proposal_result'->>'artifact_id')::uuid,params->'proposal_result'->>'sha256',
  (params->'base'->'source'->>'artifact_id')::uuid,params->'base'->'source'->>'sha256',
  params->'base'->>'canvas_hash') THEN RETURN false;END IF;
 RETURN photo.stage_v25_original_composition_task(p_tenant,p_run,p_task,p_ordinal,p_params,p_sha,p_digest,p_profile);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v25_lens_user_masked_composition_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v25_lens_user_masked_composition_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v25_lens_user_masked_composition_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
