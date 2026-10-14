-- +goose Up
-- Frozen declared protection metadata; mask origin/content are separate proofs.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_worker_protection_pins(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node jsonb; ref jsonb; field text; project uuid;
BEGIN
 IF photo.v27_semantic_worker_qualification(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_digest) IS NULL THEN RETURN NULL;END IF;
 SELECT w.project_id INTO project FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run;
 SELECT n INTO node FROM photo.runs r CROSS JOIN LATERAL jsonb_array_elements(r.config_snapshot->'v27_semantic'->'nodes') n
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND n->>'task_id'=p_task::text;
 FOR field IN SELECT unnest(ARRAY['source_protection','target_protection']) LOOP
 ref:=node->field;
 IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT(ref ?& ARRAY['artifact_id','sha256'])
 OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string' OR ref->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.project_id=project AND i.task_id=p_task AND i.contract='MaskRef'
 AND i.artifact_id=(ref->>'artifact_id')::uuid AND i.sha256=ref->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 END LOOP;
 RETURN jsonb_build_object('source_protection',node->'source_protection','target_protection',node->'target_protection');
EXCEPTION WHEN invalid_text_representation THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v27_semantic_worker_protection_pins(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_worker_protection_pins(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_worker_protection_pins(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
