-- +goose Up
-- Canonical ingest warp reads require exact verified master chain and original producer.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_import_warp(p_tenant uuid,p_workflow uuid,p_parsed text,
 p_master uuid,p_master_body text,p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;master_ref jsonb;body jsonb;master photo.artifacts%ROWTYPE;field text;role text;
BEGIN
 IF p_master_body IS NULL OR octet_length(p_master_body) NOT BETWEEN 2 AND 1048576
 OR p_contract IS NULL OR p_contract<>'DenseWarpField' OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR photo.v30_interchange_metadata_receipts(p_tenant,p_workflow,p_parsed) IS NULL THEN RETURN;END IF;
 master_ref:=jsonb_build_object('artifact_id',p_master,'sha256',encode(sha256(convert_to(p_master_body,'UTF8')),'hex'));
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(sources->'entries') AS e(value) WHERE value->>'state'='adopted' AND value->'master'=master_ref) THEN RETURN;END IF;
 SELECT * INTO master FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_master AND a.sha256=master_ref->>'sha256'
 AND a.bytes=octet_length(p_master_body) AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.created_by_task_id IS NOT NULL AND a.produced_attempt IS NOT NULL AND a.produced_fence IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF master.id IS NULL THEN RETURN;END IF;
 body:=p_master_body::jsonb;
 role:='warp_field';
 IF body->>'dtype' IS DISTINCT FROM 'float32' OR body#>>'{color,profile_id}' IS DISTINCT FROM 'work_rec2020_gamma24_v1'
 OR jsonb_typeof(body#>'{transform_chain,steps}') IS DISTINCT FROM 'array'
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(body#>'{transform_chain,steps}') step
  WHERE step->>'kind'='dense_warp' AND step->'inverse_field'=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha)) THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version NOT IN('','null') AND a.media_type='application/octet-stream' AND a.output_role=role
 AND a.created_by_task_id=master.created_by_task_id AND a.produced_attempt=master.produced_attempt AND a.produced_fence=master.produced_fence
 AND a.bytes BETWEEN 1 AND 137438953472 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_interchange_import_warp(uuid,uuid,text,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_warp(uuid,uuid,text,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_import_warp(uuid,uuid,text,uuid,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
