-- +goose Up
-- Keep the donor issuer scope unchanged; reflection has no donor proof and
-- must prove its own immutable draft, original manual pair and qualification.
ALTER FUNCTION photo.v28_restoration_params_request_scope(uuid,uuid,jsonb)
 RENAME TO v28_restoration_donor_params_request_scope;

-- +goose StatementBegin
CREATE FUNCTION photo.v28_reflection_params_request_scope(p_tenant uuid,p_run uuid,
 p_request jsonb) RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER
 SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;body jsonb;
 origin jsonb;pin jsonb;intent jsonb;expected uuid;original record;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_request) IS DISTINCT FROM 'object'
 OR pg_column_size(p_request)>131072
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_request) k)
    IS DISTINCT FROM ARRAY['actor_id','auth_session_id','node_key','origin',
     'owner_id','params_jcs','params_sha','qualification']::text[]
 OR coalesce(p_request->>'node_key','') !~ '^image[.]reflection:(0|[1-9][0-9]{0,3})$'
 OR split_part(p_request->>'node_key',':',2)::integer>=5000
 OR jsonb_typeof(p_request->'params_jcs') IS DISTINCT FROM 'string'
 OR octet_length(p_request->>'params_jcs') NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(p_request->>'params_jcs','UTF8')),'hex')
    IS DISTINCT FROM p_request->>'params_sha' THEN RETURN false;END IF;
 body:=(p_request->>'params_jcs')::jsonb;
 origin:=p_request->'origin';pin:=p_request->'qualification';
 IF jsonb_typeof(body) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(body) k)
    IS DISTINCT FROM ARRAY['base','calibration','mask','model_lock_sha',
      'protected','schema_version','strength']::text[]
 OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(origin) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(origin) k)
    IS DISTINCT FROM ARRAY['params','protected_pixel','root','target_pixel']::text[]
 OR photo.v25_layer_ref_shape(origin->'root') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(origin->'params') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(origin->'target_pixel') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(origin->'protected_pixel') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(body->'mask') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(body->'protected') IS DISTINCT FROM true
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
 expected:=photo.semantic_uuid_v5(w.id,p_request->>'node_key');
 intent:=r.config_snapshot->'v28_restoration';
 IF expected::text IS DISTINCT FROM p_request->>'owner_id'
 OR intent->>'operation' IS DISTINCT FROM 'image.reflection'
 OR intent->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR intent->>'project_id' IS DISTINCT FROM w.project_id::text
 OR intent->>'snapshot_id' IS DISTINCT FROM r.input_snapshot_id::text
 OR intent->>'actor_id' IS DISTINCT FROM r.created_by::text
 OR intent->>'auth_session_id' IS DISTINCT FROM p_request->>'auth_session_id'
 OR intent->>'run_id' IS DISTINCT FROM p_run::text
 OR intent->>'workflow_id' IS DISTINCT FROM w.id::text
 OR intent->>'ordinal' IS DISTINCT FROM split_part(p_request->>'node_key',':',2)
 OR intent->>'params_proposal_sha' IS DISTINCT FROM p_request->>'params_sha'
 OR intent->>'base_asset_version_id' IS DISTINCT FROM body->'base'->>'asset_version_id'
 OR intent->'target_pixel' IS DISTINCT FROM origin->'target_pixel'
 OR intent->'protected_pixel' IS DISTINCT FROM origin->'protected_pixel'
 OR intent->'qualification' IS DISTINCT FROM pin
 OR w.policy_sha IS DISTINCT FROM pin->>'policy_sha'
 THEN RETURN false;END IF;
 SELECT * INTO original FROM photo.v28_manual_pair_origin(p_tenant,w.project_id,
  r.input_snapshot_id,r.created_by,(p_request->>'auth_session_id')::uuid,
  body->'base',(body->'mask'->>'artifact_id')::uuid,body->'mask'->>'sha256',
  (body->'protected'->>'artifact_id')::uuid,body->'protected'->>'sha256',
  (origin->'target_pixel'->>'artifact_id')::uuid,origin->'target_pixel'->>'sha256',
  (origin->'protected_pixel'->>'artifact_id')::uuid,
  origin->'protected_pixel'->>'sha256');
 IF NOT FOUND OR original.root_id::text IS DISTINCT FROM origin->'root'->>'artifact_id'
 OR original.root_sha IS DISTINCT FROM origin->'root'->>'sha256'
 OR original.params_id::text IS DISTINCT FROM origin->'params'->>'artifact_id'
 OR original.params_sha IS DISTINCT FROM origin->'params'->>'sha256'
 OR photo.lock_v28_restoration_qualification(p_tenant,w.project_id,r.created_by,
  (p_request->>'auth_session_id')::uuid,'image.reflection',pin)
    IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_reflection_params_request_scope(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_reflection_params_request_scope(uuid,uuid,jsonb)
 FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_params_request_scope(p_tenant uuid,p_run uuid,
 p_request jsonb) RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER
 SET search_path=pg_catalog,photo AS $$
DECLARE node_key text;
BEGIN
 node_key:=p_request->>'node_key';
 IF node_key ~ '^portrait[.]glare:' THEN
  RETURN photo.v28_restoration_donor_params_request_scope(p_tenant,p_run,p_request);
 ELSIF node_key ~ '^image[.]reflection:' THEN
  RETURN photo.v28_reflection_params_request_scope(p_tenant,p_run,p_request);
 END IF;
 RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_params_request_scope(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_params_request_scope(uuid,uuid,jsonb)
 FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
