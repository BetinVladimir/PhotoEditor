-- +goose Up
-- Bind request/key before publishing; activation stores the response atomically.
-- +goose StatementBegin
CREATE FUNCTION photo.bind_v29_perspective_lines_submission(p_tenant uuid,p_project uuid,p_run uuid,
 p_actor uuid,p_session uuid,p_key text,p_request text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;idem photo.idempotency_records%ROWTYPE;v_route text;digest text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 8192 OR p_key IS NULL OR p_key !~ '^[!-~]{1,128}$'
 OR p_run IS NULL OR p_run='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN 'expired';END IF;
 request:=p_request::jsonb;
 IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(request) k) IS DISTINCT FROM ARRAY['base','schema_version','snapshot_id']::text[]
 OR request->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR photo.v28_local_portrait_source_scope(p_tenant,p_project,(request->>'snapshot_id')::uuid,p_actor,p_session,request->'base') IS DISTINCT FROM true
 THEN RETURN 'expired';END IF;
 v_route:='V29.PerspectiveLines/'||p_project::text;digest:=encode(sha256(convert_to(p_request,'UTF8')),'hex');
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,resource_id,expires_at)
 VALUES(p_tenant,'user',p_actor,v_route,p_key,digest,p_run,clock_timestamp()+interval '30 days')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND idempotency_records.route=v_route AND key=p_key FOR UPDATE;
 IF NOT FOUND THEN RETURN 'expired';END IF;
 IF idem.request_hash IS DISTINCT FROM digest OR idem.resource_id IS DISTINCT FROM p_run THEN RETURN 'conflict';END IF;
 RETURN 'bound';
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN 'expired';
END $$;
ALTER FUNCTION photo.bind_v29_perspective_lines_submission(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_v29_perspective_lines_submission(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.bind_v29_perspective_lines_submission(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;

ALTER FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) RENAME TO activate_v29_perspective_lines_pre_idempotency;
REVOKE ALL ON FUNCTION photo.activate_v29_perspective_lines_pre_idempotency(uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.activate_v29_perspective_lines(p_tenant uuid,p_run uuid,p_witness jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;
BEGIN
 -- Take request records before domain locks, matching the generic mutation
 -- contract. Bound records are immutable in this operation-specific path.
 PERFORM id FROM photo.idempotency_records WHERE tenant_id=p_tenant AND resource_id=p_run
 AND principal_type='user' AND route LIKE 'V29.PerspectiveLines/%' ORDER BY id FOR UPDATE;
 IF photo.activate_v29_perspective_lines_pre_idempotency(p_tenant,p_run,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF r.v29_perspective_lines_admission_receipt IS NULL THEN RAISE EXCEPTION 'LINE_IDEMPOTENCY_RECEIPT_MISSING' USING ERRCODE='23514';END IF;
 IF EXISTS(SELECT 1 FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant AND i.resource_id=p_run
 AND i.principal_type='user' AND i.principal_id=r.created_by
 AND i.route='V29.PerspectiveLines/'||(r.config_snapshot->'v29_perspective_line_detection'->>'project_id')
 AND i.response_status IS NOT NULL AND (i.response_status<>202 OR i.response_body IS DISTINCT FROM r.v29_perspective_lines_admission_receipt)) THEN
 RAISE EXCEPTION 'LINE_IDEMPOTENCY_RESPONSE_CONFLICT' USING ERRCODE='23514';END IF;
 UPDATE photo.idempotency_records i SET response_status=202,response_body=r.v29_perspective_lines_admission_receipt
 WHERE i.tenant_id=p_tenant AND i.resource_id=p_run AND i.principal_type='user' AND i.principal_id=r.created_by
 AND i.route='V29.PerspectiveLines/'||(r.config_snapshot->'v29_perspective_line_detection'->>'project_id') AND i.response_status IS NULL;
 RETURN true;
END $$;
ALTER FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
