-- +goose Up
-- Fresh source authority for a sealed but still blocked automatic-line task.
-- Must be called in the same transaction as eventual graph activation.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_lines_sealed_source_ready(p_tenant uuid,p_run uuid,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;image jsonb;current_source jsonb;current_array jsonb;
 ref jsonb;role text;item jsonb;index integer:=0;expected_count integer;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_witness IS NULL OR pg_column_size(p_witness)>131072 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR r.v29_perspective_lines_seal_receipt IS NULL
 OR r.v29_perspective_lines_seal_receipt->'witness' IS DISTINCT FROM p_witness THEN RETURN false;END IF;
 IF photo.seal_v29_perspective_lines_transport(p_tenant,p_run,r.v29_perspective_lines_seal_receipt->'proof',p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 plan:=r.config_snapshot->'v29_perspective_line_detection';image:=(plan->>'source_jcs')::jsonb;
 -- Repeat saved principal/snapshot authority; a durable receipt grants none.
 IF photo.save_v29_perspective_line_detection_intent(p_tenant,r.created_by,(plan->>'auth_session_id')::uuid,plan::text)
 IS DISTINCT FROM p_run THEN RETURN false;END IF;
 PERFORM holder_id FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run
 AND state='reserved' AND principal_type='user' AND principal_id=r.created_by AND bytes=4194304 AND files=0
 AND expires_at>clock_timestamp() FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 -- Lock every published control artifact against retention/cleanup changes.
 FOR d IN SELECT * FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
 AND owner_id=(plan->>'task_id')::uuid AND purpose IN('task_params','v2_input_chunk_0','v2_input_manifest')
 ORDER BY purpose FOR UPDATE LOOP
  IF d.state<>'ready' THEN RETURN false;END IF;
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=d.artifact_id AND sha256=d.sha256
  AND bytes=d.bytes AND bucket=d.bucket AND object_version=d.object_version AND object_version NOT IN('','null')
  AND object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
  AND output_role=d.purpose AND media_type='application/json' AND state='ready' AND verified_at IS NOT NULL
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT count(*) FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
 AND owner_id=(plan->>'task_id')::uuid AND purpose IN('task_params','v2_input_chunk_0','v2_input_manifest'))<>3 THEN RETURN false;END IF;
 current_source:=photo.v29_perspective_source_document(p_tenant,(plan->>'project_id')::uuid,
 (plan->>'snapshot_id')::uuid,(plan->>'actor_id')::uuid,(plan->>'auth_session_id')::uuid,plan->'base');
 IF current_source IS NULL OR p_witness->'source' IS DISTINCT FROM current_source THEN RETURN false;END IF;
 expected_count:=CASE WHEN image->'alpha'='null'::jsonb THEN 1 ELSE 2 END;
 IF jsonb_array_length(p_witness->'arrays')<>expected_count THEN RETURN false;END IF;
 FOR role,ref IN SELECT x.role,x.ref FROM (VALUES
 (0,'WorkingPixels'::text,image->'pixels'),(1,'WorkingAlpha'::text,image->'alpha')) x(ordinal,role,ref)
 WHERE x.ref IS NOT NULL AND x.ref<>'null'::jsonb ORDER BY x.ordinal LOOP
  item:=p_witness->'arrays'->index;index:=index+1;
  current_array:=photo.v29_perspective_array_source(p_tenant,(plan->>'project_id')::uuid,
  (plan->>'snapshot_id')::uuid,(plan->>'actor_id')::uuid,(plan->>'auth_session_id')::uuid,
  plan->'base',plan->>'source_jcs',(ref->>'artifact_id')::uuid,ref->>'sha256',role);
  IF current_array IS NULL OR item IS DISTINCT FROM jsonb_build_object('role',role,'object',current_array)
  OR current_array->>'bucket' IS DISTINCT FROM current_source->>'bucket' THEN RETURN false;END IF;
 END LOOP;

 -- Historical dense fields are provenance and must retain source-child authority.
 FOR role,ref IN SELECT 'InverseField/'||(n-1)::text,v->'inverse_field'
 FROM jsonb_array_elements(image->'transform_chain'->'steps') WITH ORDINALITY a(v,n)
 WHERE v->>'kind'='dense_warp' LOOP
  IF photo.lock_v29_perspective_source_child(p_tenant,(plan->>'project_id')::uuid,(plan->>'snapshot_id')::uuid,
  (plan->>'actor_id')::uuid,(plan->>'auth_session_id')::uuid,plan->'base',plan->>'source_jcs',
  (ref->>'artifact_id')::uuid,ref->>'sha256',role) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 RETURN index=expected_count;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_perspective_lines_sealed_source_ready(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_sealed_source_ready(uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_sealed_source_ready(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
