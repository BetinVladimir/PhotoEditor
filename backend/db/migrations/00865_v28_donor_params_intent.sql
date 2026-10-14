-- +goose Up
-- Dedicated prepublication intent; no task insertion, transport or activation.
ALTER TABLE photo.runs ADD COLUMN v28_donor_params_intent jsonb
 CHECK(v28_donor_params_intent IS NULL OR pg_column_size(v28_donor_params_intent)<=1048576);
-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_params_request_scope(p_tenant uuid,p_run uuid,p_request jsonb)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;body jsonb;c jsonb;h bytea;expected uuid;previous_tenant text;allowed boolean;
BEGIN
 IF jsonb_typeof(p_request) IS DISTINCT FROM 'object' OR pg_column_size(p_request)>1048576
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_request) k) IS DISTINCT FROM
 ARRAY['actor_id','auth_session_id','node_key','owner_id','params_jcs','params_sha','qualification','qualification_objects','reserved_bytes']::text[]
 OR coalesce(p_request->>'node_key','') !~ '^portrait[.]open_eyes:(0|[1-9][0-9]{0,3})$'
 OR split_part(p_request->>'node_key',':',2)::integer>=5000
 OR jsonb_typeof(p_request->'params_jcs') IS DISTINCT FROM 'string'
 OR octet_length(p_request->>'params_jcs') NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(p_request->>'params_jcs','UTF8')),'hex') IS DISTINCT FROM p_request->>'params_sha'
 OR (p_request->>'reserved_bytes')::numeric NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 body:=(p_request->>'params_jcs')::jsonb;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND kind='v2'
 AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.created_by::text IS DISTINCT FROM p_request->>'actor_id' THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
 AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 h:=public.digest(uuid_send(w.id)||convert_to(p_request->>'node_key','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected::text IS DISTINCT FROM p_request->>'owner_id' THEN RETURN false;END IF;
 SELECT d.v28_donor_issuer_context INTO c FROM photo.backend_documents d
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.purpose='v28_donor_confirmation' AND d.state='ready'
 AND d.artifact_id=(body->'confirmation'->>'artifact_id')::uuid AND d.sha256=body->'confirmation'->>'sha256' FOR SHARE;
 IF NOT FOUND OR c->>'actor_id' IS DISTINCT FROM p_request->>'actor_id'
 OR c->>'auth_session_id' IS DISTINCT FROM p_request->>'auth_session_id'
 OR c->>'project_id' IS DISTINCT FROM w.project_id::text
 OR (c->'params'-'confirmation') IS DISTINCT FROM (body-'confirmation') THEN RETURN false;END IF;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run
 AND state='reserved' AND expires_at>clock_timestamp()
 AND bytes::numeric>=(p_request->>'reserved_bytes')::numeric+65536+2097152 FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 allowed:=photo.v28_donor_confirmation_scope(p_tenant,w.project_id,(body->'base'->>'asset_version_id')::uuid,
 (body->'confirmation'->>'artifact_id')::uuid,body->'confirmation'->>'sha256',r.created_by,(p_request->>'auth_session_id')::uuid,
 (body->'base'->'source'->>'artifact_id')::uuid,body->'base'->'source'->>'sha256') IS TRUE
 AND photo.lock_v28_donor_qualification_objects(p_tenant,w.project_id,r.created_by,(p_request->>'auth_session_id')::uuid,
 p_request->'qualification',p_request->'qualification_objects') IS TRUE;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 RETURN allowed;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_donor_params_request_scope(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_params_request_scope(uuid,uuid,jsonb) FROM PUBLIC;
CREATE FUNCTION photo.save_v28_donor_params_intent(p_tenant uuid,p_run uuid,p_request jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE old jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT v28_donor_params_intent INTO old FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR photo.v28_donor_params_request_scope(p_tenant,p_run,p_request) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF old IS NOT NULL THEN RETURN old=p_request;END IF;
 UPDATE photo.runs SET v28_donor_params_intent=p_request WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v28_donor_params_intent(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v28_donor_params_intent(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v28_donor_params_intent(uuid,uuid,jsonb) TO photo_scheduler;
CREATE FUNCTION photo.v28_donor_params_document_scope(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_sha text,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;
BEGIN
 IF p_purpose IS DISTINCT FROM 'task_params' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 SELECT v28_donor_params_intent INTO request FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT FOUND OR request IS NULL OR request->>'owner_id' IS DISTINCT FROM p_owner::text
 OR request->>'params_sha' IS DISTINCT FROM p_sha OR octet_length(request->>'params_jcs') IS DISTINCT FROM p_bytes THEN RETURN false;END IF;
 RETURN photo.v28_donor_params_request_scope(p_tenant,p_run,request);
END $$;
ALTER FUNCTION photo.v28_donor_params_document_scope(uuid,uuid,uuid,text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_params_document_scope(uuid,uuid,uuid,text,text,bigint) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
