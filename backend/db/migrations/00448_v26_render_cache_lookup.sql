-- +goose Up
ALTER TABLE photo.result_cache ADD COLUMN render_inventory jsonb;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.guard_v26_render_cache() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.render_context IS NOT NULL THEN
  IF ROW(NEW.tenant_id,NEW.cache_key,NEW.result_id,NEW.valid_until,NEW.render_context,NEW.render_inventory)
   IS DISTINCT FROM ROW(OLD.tenant_id,OLD.cache_key,OLD.result_id,OLD.valid_until,OLD.render_context,OLD.render_inventory) THEN
   RAISE EXCEPTION 'Render cache entry is immutable' USING ERRCODE='42501';END IF;
 END IF;
 IF (NEW.render_context IS NOT NULL OR NEW.render_inventory IS NOT NULL) AND current_user<>'photo_fn_owner' THEN
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
 INSERT INTO photo.result_cache(tenant_id,cache_key,result_id,valid_until,render_context,render_inventory)
 VALUES(p_tenant,p_key,p_result,expiration,body,p_inventory) ON CONFLICT(tenant_id,cache_key) DO NOTHING;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.publish_v26_render_cache(uuid,uuid,integer,bigint,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.publish_v26_render_cache(uuid,uuid,integer,bigint,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.publish_v26_render_cache(uuid,uuid,integer,bigint,uuid,text,text,jsonb) TO photo_scheduler;
CREATE FUNCTION photo.lookup_v26_render_cache(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_key text,p_body text)
RETURNS TABLE(result_id uuid,root jsonb,inventory jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; cached photo.result_cache%ROWTYPE;
 producer photo.tasks%ROWTYPE; result photo.task_results%ROWTYPE; w uuid;
 expected jsonb; parents jsonb; recipe_sha text;
BEGIN
 IF p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_key IS NULL OR p_key !~ '^[0-9a-f]{64}$' OR p_body IS NULL OR octet_length(p_body)>1048576
 OR encode(sha256(convert_to(p_body,'UTF8')),'hex')<>p_key THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.cancel_requested OR t.state NOT IN('queued','running','succeeded')
 OR NOT photo.v26_render_adoption_inputs_ready(p_tenant,p_task)
 OR NOT photo.v26_render_live_qualification_ready(p_tenant,t.run_id) THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 SELECT sha256 INTO recipe_sha FROM photo.artifacts WHERE tenant_id=p_tenant AND id=t.params_artifact_id;
 SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id::text,'sha256',i.sha256)
 ORDER BY i.ordinal,i.artifact_id),'[]'::jsonb) INTO parents FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='ImageResult';
 IF jsonb_array_length(parents)>33 THEN RETURN;END IF;
 expected:=jsonb_build_object('schema_version',1,'tenant_id',p_tenant::text,'operation','render.compose','version','1',
 'params_sha256',recipe_sha,'ordered_parent_roots',parents,'execution_profile',t.execution_profile,'qualification',r.v26_qualification_pin);
 IF p_body::jsonb IS DISTINCT FROM expected THEN RETURN;END IF;
 SELECT * INTO cached FROM photo.result_cache WHERE tenant_id=p_tenant AND cache_key=p_key
 AND render_context=expected AND valid_until>clock_timestamp();
 IF cached.id IS NULL OR cached.render_inventory IS NULL OR jsonb_typeof(cached.render_inventory)<>'array'
 OR jsonb_array_length(cached.render_inventory) NOT BETWEEN 1 AND 30001 THEN RETURN;END IF;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=cached.result_id;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=result.task_id;
 SELECT id INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=producer.run_id;
 IF producer.id IS NULL OR producer.operation<>'render.compose' OR producer.operation_version<>'1'
 OR producer.cancel_requested OR producer.state<>'succeeded' OR producer.canonical_result_id IS DISTINCT FROM result.id
 OR producer.attempt IS DISTINCT FROM result.attempt OR producer.fencing_token IS DISTINCT FROM result.fencing_token
 OR jsonb_typeof(result.outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(result.outputs)<>1
 OR cached.render_inventory->0->>'artifact_id' IS DISTINCT FROM result.outputs->0->>'artifact_id'
 OR cached.render_inventory->0->>'sha256' IS DISTINCT FROM result.outputs->0->>'sha256'
 OR NOT EXISTS(SELECT 1 FROM photo.inbox i JOIN photo.run_events e
 ON e.tenant_id=i.tenant_id AND e.run_id=producer.run_id AND e.type='v2.task.adopted'
 AND e.payload->>'task_id'=producer.id::text AND e.payload->>'result_id'=result.id::text
 AND e.payload->>'sql_digest'=encode(sha256(convert_to(cached.render_inventory::text,'UTF8')),'hex')
 WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(producer.id,result.id)) THEN RETURN;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(cached.render_inventory) entry LEFT JOIN photo.artifacts a
 ON a.tenant_id=p_tenant AND a.id=(entry->>'artifact_id')::uuid AND a.sha256=entry->>'sha256'
 LEFT JOIN photo.v2_artifact_links l ON l.tenant_id=p_tenant AND l.workflow_id=w AND l.artifact_id=a.id
 AND l.sha256=a.sha256 AND l.purpose=entry->>'purpose' AND l.ordinal=(entry->>'ordinal')::integer
 WHERE a.id IS NULL OR l.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL
 OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())) THEN RETURN;END IF;
 RETURN QUERY SELECT result.id,result.outputs->0,cached.render_inventory;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.lookup_v26_render_cache(uuid,uuid,integer,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lookup_v26_render_cache(uuid,uuid,integer,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lookup_v26_render_cache(uuid,uuid,integer,bigint,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
