-- +goose Up
-- Durable request binding, independent of long object IO and graph activation.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v29_multi_intent(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_operation text,p_key text,p_intent text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent jsonb;request_hash text;route_name text;run_id uuid;h bytea;stored photo.idempotency_records%ROWTYPE;binding jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_operation IS NULL OR p_operation NOT IN('optical.panorama','optical.focus_stack')
 OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$'
 OR p_intent IS NULL OR octet_length(p_intent) NOT BETWEEN 2 AND 66560 THEN RETURN 'stale';END IF;
 intent:=p_intent::jsonb;
 IF intent->>'schema_version' IS DISTINCT FROM '1' OR jsonb_typeof(intent->'params'->'frames') IS DISTINCT FROM 'array'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(intent) k) IS DISTINCT FROM ARRAY['params','schema_version','snapshot_id']::text[]
 OR photo.v27_semantic_policy_json(intent) IS DISTINCT FROM p_intent
 OR jsonb_array_length(intent->'params'->'frames') NOT BETWEEN 2 AND CASE p_operation WHEN 'optical.panorama' THEN 20 ELSE 30 END THEN RETURN 'stale';END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN 'stale';END IF;
 FOR binding IN SELECT value FROM jsonb_array_elements(intent->'params'->'frames') LOOP
 IF photo.v28_local_portrait_source_scope(p_tenant,p_project,(intent->>'snapshot_id')::uuid,p_actor,p_session,binding) IS DISTINCT FROM true THEN RETURN 'stale';END IF;
 END LOOP;
 request_hash:=encode(sha256(convert_to(photo.v27_semantic_policy_json(jsonb_build_object('project_id',p_project,'auth_session_id',p_session,'operation',p_operation,'intent',intent)),'UTF8')),'hex');
 route_name:='V29.Multi:'||p_operation||':'||p_project;
 h:=public.digest(uuid_send(p_tenant)||convert_to('V29.Multi:'||p_operation||':'||p_project||':'||p_actor||':'||p_key,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);run_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,resource_id,expires_at)
 VALUES(p_tenant,'user',p_actor,route_name,p_key,request_hash,run_id,clock_timestamp()+interval '1 day')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO stored FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND route=route_name AND key=p_key FOR UPDATE;
 IF stored.request_hash IS DISTINCT FROM request_hash OR stored.resource_id IS DISTINCT FROM run_id THEN RETURN 'conflict';END IF;
 RETURN 'claimed';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN 'stale';
END $$;
ALTER FUNCTION photo.claim_v29_multi_intent(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v29_multi_intent(uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v29_multi_intent(uuid,uuid,uuid,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
