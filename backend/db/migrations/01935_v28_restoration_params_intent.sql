-- +goose Up
-- Bind one restoration Params publication to the same draft run, manual mask
-- producer, qualification and server-issued donor proof before object IO.
ALTER TABLE photo.runs ADD COLUMN v28_restoration_params_intent jsonb
 CHECK(v28_restoration_params_intent IS NULL OR
       pg_column_size(v28_restoration_params_intent)<=131072);

-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_params_request_scope(p_tenant uuid,p_run uuid,
 p_request jsonb) RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER
 SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;body jsonb;
 d photo.backend_documents%ROWTYPE;
 origin jsonb;pin jsonb;c jsonb;proof jsonb;h bytea;expected uuid;
 root_id uuid;root_sha text;params_id uuid;params_sha text;
BEGIN
 IF jsonb_typeof(p_request) IS DISTINCT FROM 'object'
 OR pg_column_size(p_request)>131072
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_request) k)
    IS DISTINCT FROM ARRAY['actor_id','auth_session_id','node_key','origin',
     'owner_id','params_jcs','params_sha','qualification']::text[]
 OR p_request->>'node_key' !~ '^portrait[.]glare:(0|[1-9][0-9]{0,3})$'
 OR split_part(p_request->>'node_key',':',2)::integer>=5000
 OR jsonb_typeof(p_request->'params_jcs') IS DISTINCT FROM 'string'
 OR octet_length(p_request->>'params_jcs') NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(p_request->>'params_jcs','UTF8')),'hex')
    IS DISTINCT FROM p_request->>'params_sha' THEN RETURN false;END IF;
 body:=(p_request->>'params_jcs')::jsonb;
 origin:=p_request->'origin';pin:=p_request->'qualification';
 IF jsonb_typeof(body) IS DISTINCT FROM 'object'
 OR body->>'mode' IS DISTINCT FROM 'donor'
 OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(body->'donor') IS DISTINCT FROM 'object'
 OR jsonb_typeof(origin) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(origin) k)
    IS DISTINCT FROM ARRAY['params','protected_pixel','root','target_pixel']::text[]
 OR photo.v25_layer_ref_shape(origin->'root') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(origin->'params') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(origin->'target_pixel') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(origin->'protected_pixel') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(body->'glasses_mask') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(body->'protected') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(body->'confirmation') IS DISTINCT FROM true
 OR pin->'calibration' IS DISTINCT FROM body->'calibration'
 OR pin->>'model_lock_sha' IS DISTINCT FROM body->>'model_lock_sha'
 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
  AND kind='v2' AND status='draft' AND NOT graph_sealed
  AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.created_by::text IS DISTINCT FROM p_request->>'actor_id'
 THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
  AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview'
  AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 h:=public.digest(uuid_send(w.id)||convert_to(p_request->>'node_key','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected::text IS DISTINCT FROM p_request->>'owner_id' THEN RETURN false;END IF;
 SELECT o.root_id,o.root_sha,o.params_id,o.params_sha
 INTO root_id,root_sha,params_id,params_sha
 FROM photo.v28_manual_pair_origin(p_tenant,w.project_id,r.input_snapshot_id,
  r.created_by,(p_request->>'auth_session_id')::uuid,body->'base',
  (body->'glasses_mask'->>'artifact_id')::uuid,body->'glasses_mask'->>'sha256',
  (body->'protected'->>'artifact_id')::uuid,body->'protected'->>'sha256',
  (origin->'target_pixel'->>'artifact_id')::uuid,origin->'target_pixel'->>'sha256',
  (origin->'protected_pixel'->>'artifact_id')::uuid,
  origin->'protected_pixel'->>'sha256') o;
 IF root_id IS NULL OR root_id::text IS DISTINCT FROM origin->'root'->>'artifact_id'
 OR root_sha IS DISTINCT FROM origin->'root'->>'sha256'
 OR params_id::text IS DISTINCT FROM origin->'params'->>'artifact_id'
 OR params_sha IS DISTINCT FROM origin->'params'->>'sha256'
 OR photo.lock_v28_restoration_qualification(p_tenant,w.project_id,r.created_by,
  (p_request->>'auth_session_id')::uuid,'portrait.glare',pin)
  IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT doc.* INTO d FROM photo.backend_documents doc
 JOIN photo.artifacts a ON a.tenant_id=doc.tenant_id AND a.id=doc.artifact_id
 WHERE doc.tenant_id=p_tenant AND doc.run_id=p_run AND doc.state='ready'
  AND doc.purpose='v28_donor_confirmation'
  AND doc.artifact_id=(body->'confirmation'->>'artifact_id')::uuid
  AND doc.sha256=body->'confirmation'->>'sha256'
  AND a.sha256=doc.sha256 AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.bytes=doc.bytes AND a.bucket=doc.bucket
  AND a.object_version=doc.object_version AND a.object_version NOT IN('','null')
  AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||doc.artifact_id
  AND a.media_type='application/json' AND a.output_role='v28_donor_confirmation'
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF doc,a;
 IF NOT FOUND THEN RETURN false;END IF;
 c:=d.v28_donor_issuer_context;
 IF c->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR c->>'run_id' IS DISTINCT FROM p_run::text
 OR c->>'project_id' IS DISTINCT FROM w.project_id::text
 OR c->>'actor_id' IS DISTINCT FROM p_request->>'actor_id'
 OR c->>'auth_session_id' IS DISTINCT FROM p_request->>'auth_session_id'
 OR c->'params'->'base' IS DISTINCT FROM body->'base'
 OR c->'params'->'donor' IS DISTINCT FROM body->'donor'
 OR c->'params'->'source_faces' IS DISTINCT FROM body->'source_faces'
 OR c->'params'->'donor_faces' IS DISTINCT FROM body->'donor_faces'
 OR c->'params'->'eyes_mask' IS DISTINCT FROM body->'glasses_mask'
 OR c->'params'->'protected' IS DISTINCT FROM body->'protected'
 OR c->'params'->'strength' IS DISTINCT FROM body->'strength'
 OR d.owner_id::text IS DISTINCT FROM c->'params'->'confirmation'->>'artifact_id'
 OR d.owner_id IS DISTINCT FROM expected
 OR c->'params'->'confirmation'->>'sha256' IS DISTINCT FROM d.sha256
 OR d.artifact_id::text IS DISTINCT FROM body->'confirmation'->>'artifact_id'
 THEN RETURN false;END IF;
 proof:=(c->>'proof_jcs')::jsonb;
 IF photo.lock_v28_donor_session(p_tenant,w.project_id,
  (c->>'shoot_session_id')::uuid,r.created_by,
  (p_request->>'auth_session_id')::uuid,proof,
  body->'source_faces',body->'donor_faces') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_params_request_scope(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_params_request_scope(uuid,uuid,jsonb)
 FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.save_v28_restoration_params_intent(p_tenant uuid,p_run uuid,
 p_request jsonb) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER
 SET search_path=pg_catalog,photo AS $$
DECLARE old jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 THEN RETURN false;END IF;
 SELECT v28_restoration_params_intent INTO old FROM photo.runs
 WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR photo.v28_restoration_params_request_scope(p_tenant,p_run,p_request)
 IS DISTINCT FROM true THEN RETURN false;END IF;
 IF old IS NOT NULL THEN RETURN old=p_request;END IF;
 UPDATE photo.runs SET v28_restoration_params_intent=p_request
 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.save_v28_restoration_params_intent(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v28_restoration_params_intent(uuid,uuid,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v28_restoration_params_intent(uuid,uuid,jsonb)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_params_document_scope(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_sha text,p_bytes bigint) RETURNS boolean
 LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;
BEGIN
 IF p_purpose IS DISTINCT FROM 'task_params' OR p_bytes IS NULL
 OR p_bytes NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 SELECT v28_restoration_params_intent INTO request FROM photo.runs
 WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT FOUND OR request IS NULL OR request->>'owner_id' IS DISTINCT FROM p_owner::text
 OR request->>'params_sha' IS DISTINCT FROM p_sha
 OR octet_length(request->>'params_jcs') IS DISTINCT FROM p_bytes
 THEN RETURN false;END IF;
 RETURN photo.v28_restoration_params_request_scope(p_tenant,p_run,request);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_params_document_scope(uuid,uuid,uuid,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_params_document_scope(uuid,uuid,uuid,text,text,bigint)
 FROM PUBLIC;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
