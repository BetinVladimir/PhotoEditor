-- +goose Up
-- Editor-only semantic inspection; this does not publish or accept a review.
-- +goose StatementBegin
CREATE FUNCTION photo.inspect_v30_delivery(p_tenant uuid,p_package uuid,p_actor uuid,p_session uuid,p_revision bigint) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;t photo.tasks%ROWTYPE;root_ref jsonb;params text;source jsonb;root jsonb;
 total integer;ready_count integer;omitted_count integer;matches boolean;expected jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 authority:=photo.v30_delivery_publication_authority(p_tenant,p_package,p_actor,p_session,p_revision);
 IF authority IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=(authority->>'task_id')::uuid;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO root_ref FROM photo.v2_task_inputs
 WHERE tenant_id=p_tenant AND task_id=t.id AND contract='DeliveryAssemblyInput';
 params:=format('{"assembly":{"artifact_id":"%s","sha256":"%s"},"schema_version":1}',root_ref->>'artifact_id',root_ref->>'sha256');
 source:=photo.v30_delivery_assembly_source_context(p_tenant,t.run_id,t.id,t.params_artifact_id,encode(sha256(convert_to(params,'UTF8')),'hex'),params);
 IF source IS NULL THEN RETURN NULL;END IF;
 root:=(source->>'assembly_jcs')::jsonb;
 WITH slots AS MATERIALIZED (
  SELECT slot.value FROM jsonb_array_elements(root->'shards') ref
  JOIN photo.backend_documents b ON b.tenant_id=p_tenant AND b.run_id=t.run_id AND b.owner_id=t.id
   AND b.artifact_id=(ref->>'artifact_id')::uuid AND b.sha256=ref->>'sha256' AND b.state='ready'
  CROSS JOIN LATERAL jsonb_array_elements(b.v30_delivery_document_jcs::jsonb->'items') slot
 )
 SELECT count(*),count(*) FILTER(WHERE value->>'state'='ready'),count(*) FILTER(WHERE value->>'state'='omitted'),
 bool_and(EXISTS(SELECT 1 FROM photo.delivery_items i WHERE i.tenant_id=p_tenant AND i.package_id=p_package
  AND i.ordinal=(value->>'ordinal')::integer AND i.preset_id=(value->>'preset_id')::uuid
  AND i.preset_sha=value->>'preset_sha' AND i.required=(value->>'required')::boolean
  AND i.state=value->>'state' AND i.export_id IS NOT DISTINCT FROM (value->>'export_id')::uuid
  AND i.reason IS NOT DISTINCT FROM value->>'reason'))
 INTO total,ready_count,omitted_count,matches FROM slots;
 IF matches IS DISTINCT FROM true OR total IS DISTINCT FROM (root->>'slot_count')::integer
 OR total NOT BETWEEN 1 AND 80000 OR ready_count<1 OR ready_count+omitted_count<>total
 OR total<>(SELECT count(*) FROM photo.delivery_items WHERE tenant_id=p_tenant AND package_id=p_package) THEN RETURN NULL;END IF;
 expected:=jsonb_build_object('schema_version',1,'assembly_sha',root_ref->>'sha256','snapshot_digest',root->>'snapshot_digest',
 'slot_count',total,'ready_count',ready_count,'omitted_count',omitted_count,'shards',root->'shards');
 RETURN jsonb_build_object('package_id',p_package,'revision',p_revision::text,
 'manifest',jsonb_build_object('artifact_id',authority->>'manifest_artifact_id','sha256',authority->>'manifest_sha'),
 'expected_manifest',expected);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.inspect_v30_delivery(uuid,uuid,uuid,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.inspect_v30_delivery(uuid,uuid,uuid,uuid,bigint) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.inspect_v30_delivery(uuid,uuid,uuid,uuid,bigint) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
