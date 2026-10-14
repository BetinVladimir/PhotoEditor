-- +goose Up
-- Private semantic/result binding for dedicated manifest commit.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_manifest_authority(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_params text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;root jsonb;shard_raw jsonb;shard jsonb;slot jsonb;expected jsonb;output jsonb;
 ready_count integer:=0;omitted_count integer:=0;total integer:=0;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 THEN RETURN NULL;END IF;
 source:=photo.v30_delivery_assembly_adoption_inputs(p_tenant,p_task,p_attempt,p_fence,p_result,p_params);
 IF source IS NULL THEN RETURN NULL;END IF;
 root:=(source->>'assembly_jcs')::jsonb;
 FOR shard_raw IN SELECT value FROM jsonb_array_elements(source->'shards_jcs') LOOP
  shard:=(shard_raw#>>'{}')::jsonb;
  FOR slot IN SELECT value FROM jsonb_array_elements(shard->'items') LOOP
   IF slot->>'state'='ready' THEN ready_count:=ready_count+1;
   ELSIF slot->>'state'='omitted' THEN omitted_count:=omitted_count+1;
   ELSE RETURN NULL;END IF;
   total:=total+1;
  END LOOP;
 END LOOP;
 IF ready_count<1 OR total IS DISTINCT FROM (root->>'slot_count')::integer THEN RETURN NULL;END IF;
 expected:=jsonb_build_object('schema_version',1,'assembly_sha',(p_params::jsonb)->'assembly'->>'sha256',
 'snapshot_digest',root->>'snapshot_digest','slot_count',total,'ready_count',ready_count,'omitted_count',omitted_count,'shards',root->'shards');
 IF p_body::jsonb IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 SELECT z.outputs INTO output FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=p_result AND z.task_id=p_task FOR SHARE;
 IF jsonb_typeof(output) IS DISTINCT FROM 'array' OR jsonb_array_length(output)<>1 THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(output->0->>'artifact_id')::uuid FOR SHARE;
 IF NOT FOUND OR a.sha256 IS DISTINCT FROM output->0->>'sha256'
 OR a.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex') OR a.bytes<>octet_length(p_body)
 OR a.media_type<>'application/json' OR a.output_role<>'document' OR a.created_by_task_id IS DISTINCT FROM p_task
 OR a.produced_attempt IS DISTINCT FROM p_attempt OR a.produced_fence IS DISTINCT FROM p_fence
 OR a.object_version IS NULL OR a.object_version IN('','null') OR a.verified_at IS NULL
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN RETURN NULL;END IF;
 -- Physical ready/staged upload adoption is still checked by generic private
 -- adoption in the future commit. This predicate alone never attaches a manifest.
 RETURN source||jsonb_build_object('manifest_artifact_id',a.id,'manifest_sha',a.sha256,'manifest',expected);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_manifest_authority(uuid,uuid,integer,bigint,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_manifest_authority(uuid,uuid,integer,bigint,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
