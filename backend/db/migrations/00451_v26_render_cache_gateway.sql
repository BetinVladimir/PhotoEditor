-- +goose Up
ALTER TABLE photo.result_cache ADD COLUMN render_context_jcs text;
CREATE INDEX result_cache_render_inventory_idx ON photo.result_cache USING gin(render_inventory jsonb_path_ops) WHERE render_context_jcs IS NOT NULL;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.guard_v26_render_cache() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.render_context IS NOT NULL THEN
  IF ROW(NEW.tenant_id,NEW.cache_key,NEW.result_id,NEW.valid_until,NEW.render_context,NEW.render_inventory,NEW.render_context_jcs)
   IS DISTINCT FROM ROW(OLD.tenant_id,OLD.cache_key,OLD.result_id,OLD.valid_until,OLD.render_context,OLD.render_inventory,OLD.render_context_jcs) THEN
   RAISE EXCEPTION 'Render cache entry is immutable' USING ERRCODE='42501';END IF;
 END IF;
 IF (NEW.render_context IS NOT NULL OR NEW.render_inventory IS NOT NULL OR NEW.render_context_jcs IS NOT NULL) AND current_user<>'photo_fn_owner' THEN
  RAISE EXCEPTION 'Render cache requires trusted publication' USING ERRCODE='42501';END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION photo.publish_v26_render_cache(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_key text,p_body text,p_inventory jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; w uuid; context record;
 body jsonb; expected jsonb; expiration timestamptz;
BEGIN
 IF p_body IS NULL OR octet_length(p_body)>1048576 OR p_key IS NULL
 OR p_key !~ '^[0-9a-f]{64}$' OR encode(sha256(convert_to(p_body,'UTF8')),'hex')<>p_key
 OR p_inventory IS NULL OR jsonb_typeof(p_inventory)<>'array'
 OR jsonb_array_length(p_inventory) NOT BETWEEN 1 AND 30001 OR pg_column_size(p_inventory)>16777216 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO context FROM photo.v26_render_adoption_inputs(p_tenant,p_task,p_attempt,p_fence,p_result);
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT id INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id;
 body:=p_body::jsonb;
 expected:=jsonb_build_object('schema_version',1,'tenant_id',p_tenant::text,'operation','render.compose','version','1',
  'params_sha256',context.sha256,'ordered_parent_roots',context.parent_roots,
  'execution_profile',context.execution_profile,'qualification',r.v26_qualification_pin);
 IF body IS DISTINCT FROM expected OR NOT EXISTS(SELECT 1 FROM photo.inbox i
  JOIN photo.run_events e ON e.tenant_id=i.tenant_id AND e.run_id=r.id AND e.type='v2.task.adopted'
  AND e.payload->>'task_id'=p_task::text AND e.payload->>'result_id'=p_result::text
  AND e.payload->>'sql_digest'=encode(sha256(convert_to(p_inventory::text,'UTF8')),'hex')
  WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(p_task,p_result)) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(p_inventory) entry LEFT JOIN photo.artifacts a
 ON a.tenant_id=p_tenant AND a.id=(entry->>'artifact_id')::uuid AND a.sha256=entry->>'sha256'
 LEFT JOIN photo.v2_artifact_links l ON l.tenant_id=p_tenant AND l.workflow_id=w
 AND l.artifact_id=a.id AND l.sha256=a.sha256 AND l.purpose=entry->>'purpose' AND l.ordinal=(entry->>'ordinal')::integer
 WHERE a.id IS NULL OR l.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL
 OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())) THEN RETURN false;END IF;
 SELECT least(clock_timestamp()+interval '24 hours',min(a.expires_at)) INTO expiration
 FROM jsonb_array_elements(p_inventory) entry JOIN photo.artifacts a
 ON a.tenant_id=p_tenant AND a.id=(entry->>'artifact_id')::uuid;
 INSERT INTO photo.result_cache(tenant_id,cache_key,result_id,valid_until,render_context,render_inventory,render_context_jcs)
 VALUES(p_tenant,p_key,p_result,expiration,body,p_inventory,p_body) ON CONFLICT(tenant_id,cache_key) DO NOTHING;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.publish_v26_render_cache(uuid,uuid,integer,bigint,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.publish_v26_render_cache(uuid,uuid,integer,bigint,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.publish_v26_render_cache(uuid,uuid,integer,bigint,uuid,text,text,jsonb) TO photo_scheduler;
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) RENAME TO gateway_input_before_render_cache;
REVOKE ALL ON FUNCTION photo.gateway_input_before_render_cache(uuid,integer,bigint,uuid,text,uuid) FROM photo_gateway;
CREATE FUNCTION photo.gateway_input(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,p_artifact uuid)
RETURNS TABLE(artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE permitted record;
BEGIN
 RETURN QUERY SELECT * FROM photo.gateway_input_before_render_cache(p_task,p_attempt,p_fence,p_worker,p_certificate,p_artifact);
 IF FOUND THEN RETURN;END IF;
 SELECT * INTO permitted FROM photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate);
 IF NOT FOUND OR permitted.operation<>'render.compose' THEN RETURN;END IF;
 RETURN QUERY
 SELECT a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.result_cache c
 CROSS JOIN LATERAL photo.lookup_v26_render_cache(permitted.tenant_id,p_task,p_attempt,p_fence,c.cache_key,c.render_context_jcs) hit
 CROSS JOIN LATERAL jsonb_array_elements(hit.inventory) entry
 JOIN photo.artifacts a ON a.tenant_id=permitted.tenant_id AND a.id=(entry->>'artifact_id')::uuid AND a.sha256=entry->>'sha256'
 WHERE c.tenant_id=permitted.tenant_id AND c.render_context_jcs IS NOT NULL AND c.valid_until>clock_timestamp()
 AND c.render_inventory @> jsonb_build_array(jsonb_build_object('artifact_id',p_artifact::text)) AND a.id=p_artifact LIMIT 1;
END $$;
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) TO photo_gateway;
CREATE FUNCTION photo.v26_render_worker_cache_artifacts(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text,p_key text,p_body text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,media_type text,output_role text,bucket text,object_key text,object_version text,ordinal integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.admit_v26_render_recipe(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.v26_render_cache_artifacts(p_tenant,p_task,p_attempt,p_fence,p_key,p_body);
END $$;
ALTER FUNCTION photo.v26_render_worker_cache_artifacts(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_worker_cache_artifacts(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_worker_cache_artifacts(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
