-- +goose Up
-- Compare trusted stream-verifier witnesses with current locked SQL authority.
-- No ready transition: callers must use this inside eventual seal transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_lines_inputs_ready(p_tenant uuid,p_run uuid,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;d photo.backend_documents%ROWTYPE;plan jsonb;image jsonb;
 current_source jsonb;current_array jsonb;ref jsonb;role text;item jsonb;index integer:=0;expected_count integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_witness IS NULL OR jsonb_typeof(p_witness) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_witness) k) IS DISTINCT FROM ARRAY[
 'actor_id','arrays','auth_session_id','schema_version','snapshot_id','source','task_id']::text[]
 OR p_witness->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(p_witness->'arrays') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 plan:=r.config_snapshot->'v29_perspective_line_detection';image:=(plan->>'source_jcs')::jsonb;
 IF p_witness->>'task_id' IS DISTINCT FROM plan->>'task_id'
 OR p_witness->>'snapshot_id' IS DISTINCT FROM plan->>'snapshot_id'
 OR p_witness->>'actor_id' IS DISTINCT FROM plan->>'actor_id'
 OR p_witness->>'auth_session_id' IS DISTINCT FROM plan->>'auth_session_id' THEN RETURN false;END IF;
 -- The manifest gate also replays the exact blocked task, ready Params,
 -- source/snapshot/session and run reservation authority.
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
 AND owner_id=(plan->>'task_id')::uuid AND purpose='v2_input_manifest' FOR UPDATE;
 IF NOT FOUND OR d.state<>'ready' OR d.v29_perspective_lines_transport IS NULL
 OR photo.v29_perspective_lines_transport_body_ready(p_tenant,p_run,d.owner_id,'v2_input_manifest',d.v29_perspective_lines_transport) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
 AND a.object_version NOT IN('','null') AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
 AND a.output_role='v2_input_manifest' AND a.media_type='application/json' AND a.state='ready'
 AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
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
 RETURN index=expected_count;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_perspective_lines_inputs_ready(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_inputs_ready(uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_inputs_ready(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
