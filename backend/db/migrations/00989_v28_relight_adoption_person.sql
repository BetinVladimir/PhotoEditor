-- +goose Up
-- Canonical person-mask producer metadata; original Params content must be replayed.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_adoption_person_origin(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_body text,p_mask_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE mask jsonb;root photo.artifacts%ROWTYPE;producer photo.tasks%ROWTYPE;
 original photo.artifacts%ROWTYPE;result photo.task_results%ROWTYPE;ref jsonb;role text;
BEGIN
 IF photo.v28_relight_adoption_source(p_tenant,p_task,p_attempt,p_fence,p_result,p_body) IS NULL
 OR p_mask_body IS NULL OR octet_length(p_mask_body) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 mask:=p_mask_body::jsonb;
 SELECT * INTO root FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(p_body::jsonb->'subject'->>'artifact_id')::uuid FOR SHARE;
 IF root.id IS NULL OR root.sha256 IS DISTINCT FROM p_body::jsonb->'subject'->>'sha256'
 OR root.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_mask_body,'UTF8')),'hex')
 OR root.bytes IS DISTINCT FROM octet_length(p_mask_body)::bigint
 OR root.output_role IS DISTINCT FROM 'person_mask_ref' OR root.media_type IS DISTINCT FROM 'application/json'
 OR root.state IS DISTINCT FROM 'ready' OR root.verified_at IS NULL
 OR root.object_version IS NULL OR root.object_version IN('','null')
 OR (root.expires_at IS NOT NULL AND root.expires_at<=clock_timestamp()) THEN RETURN NULL;END IF;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=root.created_by_task_id FOR SHARE;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id FOR SHARE;
 IF producer.id IS NULL OR producer.operation IS DISTINCT FROM 'mask.person' OR producer.operation_version IS DISTINCT FROM '1'
 OR producer.cancel_requested OR EXISTS(SELECT 1 FROM photo.runs pr WHERE pr.tenant_id=p_tenant AND pr.id=producer.run_id AND pr.cancel_requested)
 OR producer.state IS DISTINCT FROM 'succeeded' OR result.id IS NULL OR result.task_id IS DISTINCT FROM producer.id
 OR result.attempt IS DISTINCT FROM producer.attempt OR result.fencing_token IS DISTINCT FROM producer.fencing_token
 OR root.produced_attempt IS DISTINCT FROM result.attempt OR root.produced_fence IS DISTINCT FROM result.fencing_token
 OR NOT result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
 OR NOT EXISTS(SELECT 1 FROM photo.task_attempts a WHERE a.tenant_id=p_tenant AND a.task_id=producer.id
 AND a.attempt=result.attempt AND a.fencing_token=result.fencing_token AND a.finished_at IS NOT NULL)
 THEN RETURN NULL;END IF;
 FOR ref,role IN SELECT mask->'artifact','person_mask'::text LOOP
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.v2_task_inputs i
 ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND i.task_id=p_task AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256'
 AND a.created_by_task_id=producer.id AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token
 AND a.output_role=role AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND result.outputs @> jsonb_build_array(ref)) THEN RETURN NULL;END IF;
 END LOOP;
 IF mask->'artifact' IS NULL OR mask->'artifact'='null'::jsonb THEN RETURN NULL;END IF;
 SELECT a.* INTO original FROM photo.artifacts a JOIN photo.v2_task_inputs i
 ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND a.id=producer.params_artifact_id AND i.task_id=p_task
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 65536
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 IF original.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.artifact_id=root.id AND i.sha256=root.sha256) THEN RETURN NULL;END IF;
 IF photo.v28_relight_adoption_source(p_tenant,p_task,p_attempt,p_fence,p_result,p_body) IS NULL THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',original.id,'sha256',original.sha256,'bucket',original.bucket,
 'object_key',original.object_key,'object_version',original.object_version,'bytes',original.bytes,'media_type',original.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_relight_adoption_person_origin(uuid,uuid,integer,bigint,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_adoption_person_origin(uuid,uuid,integer,bigint,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_adoption_person_origin(uuid,uuid,integer,bigint,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
