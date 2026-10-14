-- +goose Up
-- Scoped discovery only; root/binary canonical producer proof remains mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_exposure_worker_protection_pin(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;producer uuid;pin jsonb;matches bigint;
BEGIN
 IF NOT photo.admit_semantic_local_exposure(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) THEN RETURN;END IF;
 e:=p_body::jsonb;
 SELECT t.id INTO producer FROM photo.artifacts a
 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.task_results r ON r.tenant_id=t.tenant_id AND r.task_id=t.id AND r.id=t.canonical_result_id
 AND r.attempt=t.attempt AND r.fencing_token=t.fencing_token
 WHERE a.tenant_id=p_tenant AND a.id=(e->'candidate'->>'artifact_id')::uuid
 AND a.sha256=e->'candidate'->>'sha256' AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND t.operation='semantic.transfer' AND t.operation_version='1' AND t.state='succeeded'
 AND r.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
 IF producer IS NULL THEN RETURN;END IF;
 SELECT count(*) INTO matches FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(r.config_snapshot->'v27_semantic'->'nodes')='array'
 THEN r.config_snapshot->'v27_semantic'->'nodes' ELSE '[]'::jsonb END) n
 WHERE t.tenant_id=p_tenant AND t.id=producer AND n->>'task_id'=producer::text;
 IF matches<>1 THEN RETURN;END IF;
 SELECT n->'target_protection' INTO pin FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 CROSS JOIN LATERAL jsonb_array_elements(r.config_snapshot->'v27_semantic'->'nodes') n
 WHERE t.tenant_id=p_tenant AND t.id=producer AND n->>'task_id'=producer::text;
 IF jsonb_typeof(pin) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(pin))<>2
 OR coalesce(pin->>'sha256','') !~ '^[0-9a-f]{64}$'
 OR (pin->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256 FROM photo.artifacts a
 JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.task_id=producer
 AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='MaskRef'
 WHERE a.tenant_id=p_tenant AND a.id=(pin->>'artifact_id')::uuid AND a.sha256=pin->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v27_exposure_worker_protection_pin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_exposure_worker_protection_pin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_exposure_worker_protection_pin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
