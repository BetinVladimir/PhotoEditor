-- +goose Up
-- Authenticated proposed-target root discovery, not binary/origin authority.
-- Read consumers must verify exact root bytes and canonical root/binary producer
-- and repeat current scope after object IO and in final admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_protection_pin(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_version uuid,p_candidate uuid,p_candidate_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE producer uuid;pin jsonb;matches bigint;
BEGIN
 PERFORM photo.lock_semantic_approved_params_publication(p_tenant,p_intent,p_actor,p_session,p_revision,p_target,p_target_revision,p_version,p_candidate,p_candidate_sha);
 SELECT t.id INTO producer FROM photo.artifacts a
 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.task_results r ON r.tenant_id=t.tenant_id AND r.task_id=t.id AND r.id=t.canonical_result_id
 AND r.attempt=t.attempt AND r.fencing_token=t.fencing_token
 WHERE a.tenant_id=p_tenant AND a.id=p_candidate
 AND a.sha256=p_candidate_sha AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND t.operation='semantic.transfer' AND t.operation_version='1' AND t.state='succeeded'
 AND r.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
 IF producer IS NULL THEN RETURN;END IF;
 PERFORM t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=producer FOR SHARE;
 PERFORM r.id FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 WHERE t.tenant_id=p_tenant AND t.id=producer FOR SHARE OF r;
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
ALTER FUNCTION photo.semantic_approved_protection_pin(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_protection_pin(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_approved_protection_pin(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
