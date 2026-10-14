-- +goose Up
-- Private semantic/result authority for the signed Delivery manifest. The
-- caller must still verify immutable input/output bytes before final commit.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_manifest_authority(
 p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_params text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;source jsonb;assembly jsonb;envelope jsonb;manifest jsonb;
 ref jsonb;shard jsonb;item jsonb;raw text;expected jsonb;outputs jsonb;shards jsonb:='[]'::jsonb;
 task photo.tasks%ROWTYPE;artifact photo.artifacts%ROWTYPE;
 ready_count integer:=0;omitted_count integer:=0;total integer:=0;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
  OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 scope:=photo.v30_delivery_signed_adoption_scope(p_tenant,p_task,p_attempt,p_fence,
  p_result,p_params);
 IF scope IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO task FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 source:=photo.v30_delivery_signed_runtime_documents(p_tenant,task.run_id,p_task,
  task.params_artifact_id,scope->>'params_sha',p_params);
 IF source IS NULL THEN RETURN NULL;END IF;
 envelope:=(source->>'envelope_jcs')::jsonb;
 assembly:=(source->>'assembly_jcs')::jsonb;
 FOR ref IN SELECT value FROM jsonb_array_elements(assembly->'shards') LOOP
  raw:=photo.v30_delivery_signed_ready_document(p_tenant,task.run_id,p_task,
   'v30_delivery_slots_'||lpad((total/500)::text,3,'0'),ref);
  IF raw IS NULL THEN RETURN NULL;END IF;
  shards:=shards||jsonb_build_array(raw);
  shard:=raw::jsonb;
  FOR item IN SELECT value FROM jsonb_array_elements(shard->'items') LOOP
   IF item->>'state'='ready' THEN ready_count:=ready_count+1;
   ELSIF item->>'state'='omitted' THEN omitted_count:=omitted_count+1;
   ELSE RETURN NULL;END IF;
   total:=total+1;
  END LOOP;
 END LOOP;
 IF ready_count<1 OR total IS DISTINCT FROM (assembly->>'slot_count')::integer
  OR total<>ready_count+omitted_count THEN RETURN NULL;END IF;
 expected:=jsonb_build_object('schema_version',2,
  'envelope_sha',(p_params::jsonb)->'assembly'->>'sha256',
  'assembly',envelope->'assembly','signing_closure',envelope->'signing_closure',
  'snapshot_digest',assembly->>'snapshot_digest','slot_count',total,
  'ready_count',ready_count,'omitted_count',omitted_count,'shards',assembly->'shards');
 manifest:=p_body::jsonb;
 IF manifest IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 SELECT z.outputs INTO outputs FROM photo.task_results z WHERE z.tenant_id=p_tenant
  AND z.id=p_result AND z.task_id=p_task AND z.attempt=p_attempt
  AND z.fencing_token=p_fence FOR SHARE;
 IF jsonb_typeof(outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(outputs)<>1
  THEN RETURN NULL;END IF;
 SELECT * INTO artifact FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=(outputs->0->>'artifact_id')::uuid FOR SHARE;
 IF NOT FOUND OR artifact.sha256 IS DISTINCT FROM outputs->0->>'sha256'
  OR artifact.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex')
  OR artifact.bytes<>octet_length(p_body) OR artifact.media_type<>'application/json'
  OR artifact.output_role<>'document' OR artifact.created_by_task_id IS DISTINCT FROM p_task
  OR artifact.produced_attempt IS DISTINCT FROM p_attempt
  OR artifact.produced_fence IS DISTINCT FROM p_fence
  OR artifact.object_version IS NULL OR artifact.object_version IN('','null')
  OR artifact.verified_at IS NULL OR
   (artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp())
  THEN RETURN NULL;END IF;
 RETURN source||jsonb_build_object('manifest_artifact_id',artifact.id,
  'manifest_sha',artifact.sha256,'manifest',expected,'run_id',task.run_id,
  'shards_jcs',shards);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_manifest_authority(uuid,uuid,integer,bigint,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_manifest_authority(uuid,uuid,integer,bigint,uuid,text,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
