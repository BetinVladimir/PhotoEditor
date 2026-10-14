-- +goose Up
-- Read authority only; does not activate or adopt metadata tasks.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_result_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;e photo.graph_expansions%ROWTYPE;
 z photo.task_results%ROWTYPE;proof jsonb;request jsonb;cfg jsonb;approved text[];ref jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.operation<>'metadata.write' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt
 OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result
 OR p_result IS NULL OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed
 OR t.id<>photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'metadata.write:0') THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=r.id
 AND kind='v30.delivery.metadata/1' AND state='closed';
 IF NOT FOUND OR e.expected_count<>1 OR e.v30_metadata_proof_jcs IS NULL THEN RETURN false;END IF;
 proof:=e.v30_metadata_proof_jcs::jsonb;request:=r.config_snapshot->'v30_delivery_export';
 cfg:=(request->>'config_jcs')::jsonb;
 IF proof->>'task_id' IS DISTINCT FROM t.id::text OR proof->'binding' IS DISTINCT FROM request->'binding'
 OR proof->>'policy' IS DISTINCT FROM cfg->>'metadata_policy' THEN RETURN false;END IF;
 IF proof->>'policy'='approved_exif' THEN
  IF jsonb_typeof(r.config_snapshot->'v30_metadata_approved_fields') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved
  FROM jsonb_array_elements_text(r.config_snapshot->'v30_metadata_approved_fields');
 ELSE approved:=ARRAY[]::text[];END IF;
 IF proof->'approved_fields' IS DISTINCT FROM to_jsonb(approved)
 OR photo.v30_metadata_write_plan_valid(t.id,proof->'encoded',proof->>'encoded_jcs',proof->>'frozen_jcs',
 proof->>'frozen_sha256',proof->'binding',proof->'metadata_source',proof->>'metadata_jcs',proof->>'policy',approved,
 proof->>'projection_jcs',proof->>'params_jcs') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions parent WHERE parent.tenant_id=p_tenant
 AND parent.run_id=(request->>'parent_run_id')::uuid AND parent.kind='v2.delivery/1' AND parent.state='open'
 AND photo.v30_delivery_expansion_ready(parent.id) IS TRUE) THEN RETURN false;END IF;
 IF proof->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(t.id,'delivery:metadata:params:1'),
 'sha256',encode(sha256(convert_to(proof->>'params_jcs','UTF8')),'hex'))
 OR proof->'projection' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(t.id,'delivery:metadata:projection:1'),
 'sha256',encode(sha256(convert_to(proof->>'projection_jcs','UTF8')),'hex')) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks producer JOIN photo.task_results result
 ON result.tenant_id=producer.tenant_id AND result.task_id=producer.id AND result.id=producer.canonical_result_id
 WHERE producer.tenant_id=p_tenant AND producer.run_id=r.id AND producer.id=e.parent_task_id
 AND producer.operation='export.encode' AND producer.operation_version='1' AND producer.state='succeeded'
 AND NOT producer.cancel_requested AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 AND result.outputs @> jsonb_build_array(proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact')) THEN RETURN false;END IF;
 IF t.params_artifact_id::text IS DISTINCT FROM proof->'params'->>'artifact_id'
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOREACH ref IN ARRAY ARRAY[proof->'params',proof->'projection',proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact'] LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a
   ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
   WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(ref->>'artifact_id')::uuid
   AND i.sha256=ref->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=p_result;
 IF NOT FOUND OR z.attempt IS DISTINCT FROM p_attempt OR z.fencing_token IS DISTINCT FROM p_fence
 OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(z.outputs)<>3 THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(z.outputs) output WHERE NOT EXISTS(
  SELECT 1 FROM photo.artifacts a JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
  WHERE a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
  AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready'
  AND u.sha256=a.sha256 AND u.role=a.output_role
  AND a.output_role IN('encoded_manifest','encoded_image','metadata_report'))) THEN RETURN false;END IF;
 RETURN (SELECT count(DISTINCT a.output_role)=3 FROM jsonb_array_elements(z.outputs) output
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_result_ready(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_result_ready(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

CREATE FUNCTION photo.v30_metadata_result_artifact(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_artifact uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,role text,source text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,
 CASE WHEN u.artifact_id IS NOT NULL THEN u.role WHEN a.output_role='encoded_image' THEN 'encoded_image'
 WHEN EXISTS(SELECT 1 FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.artifact_id=a.id
 AND b.owner_id=p_task AND b.purpose='v30_metadata_projection' AND b.state='ready') THEN 'v30_metadata_projection' ELSE NULL END,
 CASE WHEN u.artifact_id IS NOT NULL THEN 'output' ELSE 'input' END
 FROM photo.artifacts a LEFT JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
 AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready'
 WHERE photo.v30_metadata_result_ready(p_tenant,p_task,p_attempt,p_fence,p_result)
 AND a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND ((u.artifact_id IS NOT NULL AND EXISTS(SELECT 1 FROM photo.task_results z
 CROSS JOIN LATERAL jsonb_array_elements(z.outputs) output WHERE z.tenant_id=p_tenant AND z.id=p_result
 AND z.task_id=p_task AND output->>'artifact_id'=a.id::text AND output->>'sha256'=a.sha256))
 OR (u.artifact_id IS NULL AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
 AND i.task_id=p_task AND i.artifact_id=a.id AND i.sha256=a.sha256)));
$$;
CREATE FUNCTION photo.v30_metadata_result_outputs(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,state text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT u.artifact_id,u.sha256,u.state FROM photo.output_uploads u
 WHERE u.tenant_id=p_tenant AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence
 AND photo.v30_metadata_result_ready(p_tenant,p_task,p_attempt,p_fence,p_result);
$$;
ALTER FUNCTION photo.v30_metadata_result_artifact(uuid,uuid,integer,bigint,uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v30_metadata_result_outputs(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_result_artifact(uuid,uuid,integer,bigint,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
REVOKE ALL ON FUNCTION photo.v30_metadata_result_outputs(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_metadata_result_artifact(uuid,uuid,integer,bigint,uuid,uuid) TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.v30_metadata_result_outputs(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
