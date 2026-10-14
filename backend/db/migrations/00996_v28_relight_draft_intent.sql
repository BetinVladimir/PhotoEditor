-- +goose Up
-- Frozen preliminary intent only: no publication, transport or activation grant.
ALTER TABLE photo.runs ADD COLUMN v28_relight_intent jsonb
 CHECK(v28_relight_intent IS NULL OR pg_column_size(v28_relight_intent)<=1048576);
-- +goose StatementBegin
CREATE FUNCTION photo.save_v28_relight_intent(p_tenant uuid,p_run uuid,p_request jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; body jsonb; profile jsonb; inputs jsonb;
 old jsonb; h bytea; expected uuid; label text; field text; ref jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_request) IS DISTINCT FROM 'object' OR pg_column_size(p_request)>1048576
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_request) k) IS DISTINCT FROM
 ARRAY['actor_id','auth_session_id','inputs_jcs','inputs_sha','node_key','owner_id','params_jcs','params_sha','profile_jcs','profile_sha','qualification','reserved_bytes']::text[]
 OR coalesce(p_request->>'node_key','') !~ '^image[.]relight:(0|[1-9][0-9]{0,3})$'
 OR split_part(p_request->>'node_key',':',2)::integer>=5000
 OR jsonb_typeof(p_request->'reserved_bytes') IS DISTINCT FROM 'number'
 OR (p_request->>'reserved_bytes')::numeric NOT BETWEEN 1 AND 9007199254740991
 OR (p_request->>'reserved_bytes')::numeric<>trunc((p_request->>'reserved_bytes')::numeric)
 THEN RETURN false;END IF;
 FOREACH label IN ARRAY ARRAY['params','profile','inputs'] LOOP
 IF jsonb_typeof(p_request->(label||'_jcs')) IS DISTINCT FROM 'string'
 OR octet_length(p_request->>(label||'_jcs')) NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(p_request->>(label||'_jcs'),'UTF8')),'hex') IS DISTINCT FROM p_request->>(label||'_sha')
 THEN RETURN false;END IF;
 END LOOP;
 body:=(p_request->>'params_jcs')::jsonb;profile:=(p_request->>'profile_jcs')::jsonb;inputs:=(p_request->>'inputs_jcs')::jsonb;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR body->'schema_version'<>'1'::jsonb
 OR NOT(body ?& ARRAY['schema_version','base','strength','depth','subject','protected','lights','calibration'])
 OR body-ARRAY['schema_version','base','strength','depth','subject','protected','lights','calibration']<>'{}'::jsonb
 OR jsonb_typeof(inputs) IS DISTINCT FROM 'array' OR jsonb_array_length(inputs) NOT BETWEEN 10 AND 512
 OR profile->>'backend' IS DISTINCT FROM 'cpu' OR profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR profile->'relight_qualification' IS DISTINCT FROM p_request->'qualification'
 OR profile->>'relight_runtime_sha' IS DISTINCT FROM p_request->'qualification'->>'runtime_lock_sha'
 OR body->'calibration' IS DISTINCT FROM p_request->'qualification'->'calibration'
 OR profile->'resource_estimate'->>'output_bytes' IS DISTINCT FROM p_request->>'reserved_bytes'
 THEN RETURN false;END IF;
 FOREACH field IN ARRAY ARRAY['source','depth','subject','protected','calibration','report'] LOOP
 ref:=CASE field WHEN 'source' THEN body->'base'->'source' WHEN 'report' THEN p_request->'qualification'->'report' ELSE body->field END;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(inputs) i WHERE i->'ref'=ref
 AND i->>'contract'=CASE field WHEN 'source' THEN 'WorkingImage' WHEN 'depth' THEN 'RelativeDepth'
 WHEN 'calibration' THEN 'Calibration' WHEN 'report' THEN 'QualificationReport' ELSE 'MaskRef' END)
 THEN RETURN false;END IF;
 END LOOP;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR r.created_by::text IS DISTINCT FROM p_request->>'actor_id' THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
 AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 h:=public.digest(uuid_send(w.id)||convert_to(p_request->>'node_key','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected::text IS DISTINCT FROM p_request->>'owner_id' THEN RETURN false;END IF;
 IF photo.lock_v28_relight_source(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,(p_request->>'auth_session_id')::uuid,body->'base') IS DISTINCT FROM true
 OR photo.lock_v28_relight_qualification(p_tenant,w.project_id,r.created_by,(p_request->>'auth_session_id')::uuid,p_request->'qualification') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 old:=r.v28_relight_intent;
 IF old IS NOT NULL THEN RETURN old=p_request;END IF;
 UPDATE photo.runs SET v28_relight_intent=p_request WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v28_relight_intent(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v28_relight_intent(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v28_relight_intent(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
