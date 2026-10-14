-- +goose Up
-- Full mapped physical locks are retained by the dedicated Go caller.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_replacement_detector_sealed_source_ready(p_tenant uuid,p_run uuid,p_proof jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE descriptor jsonb;image jsonb;ref jsonb;current_object jsonb;role text;idx integer:=0;owner uuid;doc photo.backend_documents%ROWTYPE;
BEGIN
 descriptor:=(p_proof->>'descriptor_jcs')::jsonb;owner:=(descriptor->>'task_id')::uuid;
 IF photo.read_v29_replacement_detector_seal_receipt(p_tenant,p_run,p_proof->>'descriptor_jcs') IS NULL
 OR (SELECT v29_replacement_detector_seal_receipt->'witness' FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run) IS DISTINCT FROM p_witness
 OR jsonb_typeof(p_witness) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_witness) k) IS DISTINCT FROM ARRAY['actor_id','arrays','auth_session_id','schema_version','snapshot_id','source','task_id']::text[]
 OR p_witness->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_witness->>'task_id' IS DISTINCT FROM owner::text
 OR p_witness->>'snapshot_id' IS DISTINCT FROM p_proof->>'snapshot_id'
 OR p_witness->>'actor_id' IS DISTINCT FROM descriptor->>'actor_id'
 OR p_witness->>'auth_session_id' IS DISTINCT FROM descriptor->>'auth_session_id'
 OR jsonb_typeof(p_witness->'arrays') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 ref:=descriptor->'base'->'source';
 current_object:=photo.v29_replacement_detector_input_object(p_tenant,p_run,owner,'WorkingImage',(ref->>'artifact_id')::uuid,ref->>'sha256',p_proof);
 IF current_object IS NULL OR p_witness->'source' IS DISTINCT FROM current_object THEN RETURN false;END IF;
 PERFORM holder_id FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run
 AND state='reserved' AND principal_type='user' AND principal_id=(descriptor->>'actor_id')::uuid
 AND bytes=4194304 AND files=0 AND expires_at>clock_timestamp() FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 FOR doc IN SELECT * FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=owner
 AND purpose IN('task_params','v2_input_chunk_0','v2_input_manifest') ORDER BY purpose FOR UPDATE LOOP
  IF doc.state<>'ready' THEN RETURN false;END IF;
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=doc.artifact_id AND sha256=doc.sha256
  AND bytes=doc.bytes AND bucket=doc.bucket AND object_version=doc.object_version AND object_version NOT IN('','null')
  AND object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||doc.artifact_id
  AND output_role=doc.purpose AND media_type='application/json' AND state='ready' AND verified_at IS NOT NULL
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT count(*) FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=owner
 AND purpose IN('task_params','v2_input_chunk_0','v2_input_manifest'))<>3 THEN RETURN false;END IF;
 image:=(descriptor->>'source_jcs')::jsonb;
 FOR role,ref IN SELECT x.role,x.ref FROM (VALUES (0,'WorkingPixels'::text,image->'pixels'),(1,'WorkingAlpha'::text,image->'alpha')) x(n,role,ref)
 WHERE x.ref IS NOT NULL AND x.ref<>'null'::jsonb ORDER BY n LOOP
  current_object:=photo.v29_replacement_detector_input_object(p_tenant,p_run,owner,role,(ref->>'artifact_id')::uuid,ref->>'sha256',p_proof);
  IF current_object IS NULL OR p_witness->'arrays'->idx IS DISTINCT FROM jsonb_build_object('role',role,'object',current_object)
   OR current_object->>'bucket' IS DISTINCT FROM p_witness->'source'->>'bucket' THEN RETURN false;END IF;
  idx:=idx+1;
 END LOOP;
 RETURN idx BETWEEN 1 AND 2 AND jsonb_array_length(p_witness->'arrays')=idx;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_replacement_detector_sealed_source_ready(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_replacement_detector_sealed_source_ready(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v29_replacement_detector(p_tenant uuid,p_run uuid,p_proof jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE descriptor jsonb;
BEGIN
 IF photo.v29_replacement_detector_sealed_source_ready(p_tenant,p_run,p_proof,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 descriptor:=(p_proof->>'descriptor_jcs')::jsonb;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(descriptor->>'task_id')::uuid
 AND operation='optical.perspective_lines' AND operation_version='1' AND node_key='optical.perspective_lines'
 AND resource_class='cpu' AND state='blocked' AND attempt=0 AND NOT cancel_requested)
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run) THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v29_lines(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.activate_v29_replacement_detector(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v29_replacement_detector(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.activate_v29_replacement_detector(uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
