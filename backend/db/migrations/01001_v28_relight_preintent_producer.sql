-- +goose Up
-- Read-only discovery before immutable intent creation; not task admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_preintent_producer(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,p_selected jsonb,p_kind text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected photo.artifacts%ROWTYPE;root photo.artifacts%ROWTYPE;original photo.artifacts%ROWTYPE;
 producer photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;roots uuid[];expected_role text;expected_operation text;
 a photo.artifacts%ROWTYPE;answer jsonb:='{}';label text;
BEGIN
 IF p_kind IS NULL OR p_kind NOT IN('depth','person','manual_target','manual_guard')
 OR photo.lock_v28_relight_source(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 expected_role:=CASE p_kind WHEN 'depth' THEN 'relative_depth_ref' WHEN 'person' THEN 'person_mask_ref' WHEN 'manual_target' THEN 'mask_target_ref' ELSE 'mask_guard_ref' END;
 expected_operation:=CASE p_kind WHEN 'depth' THEN 'depth.predict' WHEN 'person' THEN 'mask.person' ELSE 'mask.rasterize' END;
 SELECT * INTO selected FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(p_selected->>'artifact_id')::uuid AND sha256=p_selected->>'sha256' FOR SHARE;
 IF selected.id IS NULL OR selected.output_role IS DISTINCT FROM expected_role THEN RETURN NULL;END IF;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=selected.created_by_task_id FOR SHARE;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id FOR SHARE;
 IF producer.id IS NULL OR producer.operation IS DISTINCT FROM expected_operation OR producer.operation_version IS DISTINCT FROM '1'
 OR producer.state IS DISTINCT FROM 'succeeded' OR producer.cancel_requested
 OR result.id IS NULL OR result.task_id IS DISTINCT FROM producer.id
 OR result.attempt IS DISTINCT FROM producer.attempt OR result.fencing_token IS DISTINCT FROM producer.fencing_token
 OR selected.produced_attempt IS DISTINCT FROM result.attempt OR selected.produced_fence IS DISTINCT FROM result.fencing_token
 OR NOT result.outputs @> jsonb_build_array(p_selected)
 OR NOT EXISTS(SELECT 1 FROM photo.task_attempts t WHERE t.tenant_id=p_tenant AND t.task_id=producer.id AND t.attempt=result.attempt AND t.fencing_token=result.fencing_token AND t.finished_at IS NOT NULL) THEN RETURN NULL;END IF;
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=producer.run_id AND NOT r.cancel_requested AND EXISTS
 (SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id
 AND i.asset_version_id=(p_base->>'asset_version_id')::uuid AND i.artifact_id=(p_base->'source'->>'artifact_id')::uuid AND i.sha256=p_base->'source'->>'sha256') FOR SHARE OF r;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO original FROM photo.artifacts WHERE tenant_id=p_tenant AND id=producer.params_artifact_id FOR SHARE;
 IF p_kind IN('depth','person') THEN root:=selected;
 ELSE
 -- Ambiguous canonical roots cannot be resolved by arbitrary row ordering.
 SELECT array_agg(ar.id ORDER BY ar.id) INTO roots FROM photo.artifacts ar
 WHERE ar.tenant_id=p_tenant AND ar.created_by_task_id=producer.id AND ar.output_role='rasterized_mask'
 AND ar.produced_attempt=result.attempt AND ar.produced_fence=result.fencing_token
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',ar.id,'sha256',ar.sha256));
 IF cardinality(roots) IS DISTINCT FROM 1 THEN RETURN NULL;END IF;
 SELECT * INTO root FROM photo.artifacts WHERE tenant_id=p_tenant AND id=roots[1] FOR SHARE;
 PERFORM i.task_id FROM photo.interactive_tasks i WHERE i.tenant_id=p_tenant AND i.task_id=producer.id
 AND i.kind='mask_manual' AND i.operation='mask.rasterize' AND i.source_artifact_id=(p_base->'source'->>'artifact_id')::uuid
 AND i.params->'image'=p_base->'source' AND i.params_sha256=original.sha256 FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 END IF;
 FOR label IN SELECT unnest(ARRAY['selected','root','params']) LOOP
 a:=CASE label WHEN 'selected' THEN selected WHEN 'root' THEN root ELSE original END;
 IF a.id IS NULL OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.media_type IS DISTINCT FROM 'application/json'
 OR a.bytes NOT BETWEEN 2 AND (CASE WHEN label='params' AND p_kind IN('depth','person') THEN 65536 ELSE 1048576 END)
 OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN RETURN NULL;END IF;
 answer:=answer||jsonb_build_object(label,jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type));
 END LOOP;
 IF photo.lock_v28_relight_source(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN answer;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_relight_preintent_producer(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_preintent_producer(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_preintent_producer(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
