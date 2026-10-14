-- +goose Up
-- Exact saved Params, existing immutable document lifecycle; no queue activation.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v29_perspective_lines_transport(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_sha text,p_size bigint,p_bucket text,p_body text)
RETURNS TABLE(artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; d photo.backend_documents%ROWTYPE; plan jsonb;
 capacity numeric; used numeric; aid uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_owner IS NULL OR p_purpose IS NULL OR p_purpose NOT IN('v2_input_chunk_0','v2_input_manifest')
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536
 OR p_size IS DISTINCT FROM octet_length(p_body)::bigint
 OR p_sha IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex')
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.status<>'draft'
 OR r.graph_sealed OR r.cancel_requested THEN RETURN;END IF;
 plan:=r.config_snapshot->'v29_perspective_line_detection';
 IF plan->>'task_id' IS DISTINCT FROM p_owner::text
 OR photo.v29_perspective_lines_transport_body_ready(p_tenant,p_run,p_owner,p_purpose,p_body) IS DISTINCT FROM true THEN RETURN;END IF;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run
 AND state='reserved' AND principal_type='user' AND principal_id=r.created_by AND bytes=4194304 AND files=0
 AND expires_at>clock_timestamp() FOR SHARE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run
 AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_size OR d.bucket IS DISTINCT FROM p_bucket
  OR d.state NOT IN('pending','ready') OR d.v29_perspective_lines_transport IS DISTINCT FROM p_body THEN RETURN;END IF;
  IF d.state='ready' AND NOT EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=p_sha AND a.bytes=p_size
   AND a.bucket=p_bucket AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
   AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
   AND a.output_role=p_purpose AND a.media_type='application/json' AND a.state='ready'
   AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN;END IF;
  RETURN QUERY SELECT d.artifact_id,d.token,d.state='ready';RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used+p_size>capacity OR used+p_size>9007199254740991 THEN RETURN;END IF;
 -- Document reservation is separate from the output run reservation and is
 -- consumed by document completion, matching the existing publisher protocol.
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,v29_perspective_lines_transport)
 VALUES(p_tenant,p_run,p_owner,p_purpose,aid,gen_random_uuid(),p_sha,p_size,p_bucket,p_body)
 RETURNING * INTO d;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_size WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT d.artifact_id,d.token,false;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.reserve_v29_perspective_lines_transport(uuid,uuid,uuid,text,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v29_perspective_lines_transport(uuid,uuid,uuid,text,text,bigint,text,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.reserve_v29_perspective_lines_transport(uuid,uuid,uuid,text,text,bigint,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
