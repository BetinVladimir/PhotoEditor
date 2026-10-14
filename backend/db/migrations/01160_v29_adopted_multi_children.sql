-- +goose Up
-- Exact first-visit adopted occurrence. A typed path is not a wildcard query:
-- '*' is the literal array marker retained by the schema adoption inventory.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_adopted_multi_child_object(p_tenant uuid,p_project uuid,p_task uuid,
 p_actor uuid,p_session uuid,p_operation text,p_root uuid,p_root_sha text,
 p_artifact uuid,p_sha text,p_path text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v29_adopted_multi_binding(p_tenant,p_project,p_task,p_actor,p_session,p_operation) b
 JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=p_task
 JOIN photo.v2_artifact_links l ON l.tenant_id=p_tenant AND l.workflow_id=b.workflow_id AND l.project_id=p_project
 AND l.artifact_id=p_artifact AND l.sha256=p_sha
 AND l.purpose=encode(sha256(convert_to('tasks/'||p_task::text||'/'||p_path,'UTF8')),'hex')
 JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
 WHERE b.artifact_id=p_root AND b.sha256=p_root_sha AND p_artifact<>p_root
 AND coalesce(p_sha,'') ~ '^[0-9a-f]{64}$'
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.bytes>0
 AND ((p_contract='WorkingImage' AND p_path='$/working_image' AND a.output_role='working_image' AND a.media_type='application/json' AND a.bytes<=1048576)
 OR (p_contract='ImageReport' AND p_path='$/report' AND a.output_role='report' AND a.media_type='application/json' AND a.bytes<=65536)
 OR (p_contract='MaskRef' AND p_path='$/changed_mask' AND a.output_role='mask' AND a.media_type='application/json' AND a.bytes<=1048576)
 OR (p_contract='Artifact' AND p_path='$/report/registrations/*' AND a.output_role='registration' AND a.media_type='application/json' AND a.bytes<=65536)
 OR (p_contract='WorkingPixels' AND p_path='$/working_image/pixels' AND a.output_role='pixels' AND a.media_type='application/x-npy' AND a.bytes<=2400008192)
 OR (p_contract='WorkingAlpha' AND p_path='$/working_image/alpha' AND a.output_role='alpha' AND a.media_type='application/x-npy' AND a.bytes<=800008192)
 OR (p_contract='MaskPixels' AND p_path='$/changed_mask/artifact' AND a.output_role='mask_pixels' AND a.media_type='application/x-npy' AND a.bytes<=800008192)
 OR (p_contract='DenseWarpField' AND p_path IN('$/working_image/transform_chain/steps/*/inverse_field','$/changed_mask/transform_chain/steps/*/inverse_field','$/report/registrations/*/inverse_field')
 AND a.output_role='warp_field' AND a.media_type='application/x-npy' AND a.bytes<=1600008192));
$$;
ALTER FUNCTION photo.v29_adopted_multi_child_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_adopted_multi_child_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_adopted_multi_child_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd

-- Exact input edge from one ordered original. Original inputs are not adopted
-- outputs, so workflow-link membership alone must never authorize their reads.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_adopted_multi_original_object(p_tenant uuid,p_project uuid,p_task uuid,
 p_actor uuid,p_session uuid,p_operation text,p_root uuid,p_root_sha text,p_ordinal integer,
 p_artifact uuid,p_sha text,p_role text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b record;plan jsonb;params jsonb;source jsonb;edge jsonb;input_contract text;v_step integer;
BEGIN
 IF p_ordinal IS NULL OR p_ordinal<0 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 SELECT * INTO b FROM photo.v29_adopted_multi_binding(p_tenant,p_project,p_task,p_actor,p_session,p_operation);
 IF NOT FOUND OR b.artifact_id IS DISTINCT FROM p_root OR b.sha256 IS DISTINCT FROM p_root_sha THEN RETURN;END IF;
 params:=b.params_jcs::jsonb;
 IF p_ordinal>=jsonb_array_length(params->'frames') THEN RETURN;END IF;
 SELECT r.config_snapshot->'v29_multi_plan' INTO plan FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=b.run_id;
 source:=(plan->'sources_jcs'->>p_ordinal)::jsonb;input_contract:='NpyArray';
 IF p_role='WorkingImage' THEN edge:=params->'frames'->p_ordinal->'source';input_contract:='WorkingImage';
 ELSIF p_role='WorkingPixels' THEN edge:=source->'pixels';
 ELSIF p_role='WorkingAlpha' THEN edge:=source->'alpha';
 ELSIF p_role ~ '^InverseField/(0|[1-9][0-9]{0,2})$' THEN
 v_step:=split_part(p_role,'/',2)::integer;
 IF v_step>=128 OR source->'transform_chain'->'steps'->v_step->>'kind' IS DISTINCT FROM 'dense_warp' THEN RETURN;END IF;
 edge:=source->'transform_chain'->'steps'->v_step->'inverse_field';
 ELSE RETURN;END IF;
 IF edge->>'artifact_id' IS DISTINCT FROM p_artifact::text OR edge->>'sha256' IS DISTINCT FROM p_sha THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type FROM photo.artifacts a
 JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.task_id=p_task AND i.project_id=p_project
 AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract=input_contract
 WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.bytes>0
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND ((p_role='WorkingImage' AND a.output_role='working_image' AND a.media_type='application/json' AND a.bytes<=1048576)
 OR (p_role='WorkingPixels' AND a.output_role IN('pixels','working_rgb') AND a.media_type IN('application/x-npy','application/octet-stream') AND a.bytes<=2400008192)
 OR (p_role='WorkingAlpha' AND a.output_role IN('alpha','working_alpha') AND a.media_type IN('application/x-npy','application/octet-stream') AND a.bytes<=800008192)
 OR (p_role LIKE 'InverseField/%' AND a.output_role='warp_field' AND a.media_type='application/x-npy' AND a.bytes<=1600008192));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v29_adopted_multi_original_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text,integer,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_adopted_multi_original_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text,integer,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_adopted_multi_original_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text,integer,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
