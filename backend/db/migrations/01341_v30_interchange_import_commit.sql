-- +goose Up
-- Import cannot use generic adoption without independent semantic reconstruction.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_import;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_import(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='v2.interchange.import') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_import(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
CREATE FUNCTION photo.adopt_v30_interchange_import(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_inventory jsonb,p_digest text,p_params text,p_root text,p_report text,p_shards jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;authority jsonb;root jsonb;report jsonb;params jsonb;output jsonb;
 shard text;pin jsonb;ordinal integer:=0;entries integer:=0;total bigint;source jsonb;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
 OR p_root IS NULL OR octet_length(p_root) NOT BETWEEN 2 AND 8388608
 OR p_report IS NULL OR octet_length(p_report) NOT BETWEEN 2 AND 8388608
 OR jsonb_typeof(p_shards) IS DISTINCT FROM 'array' OR jsonb_array_length(p_shards)>50 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 authority:=photo.v30_interchange_import_adoption_context(p_tenant,p_task,p_attempt,p_fence,p_result);
 IF authority IS NULL OR encode(sha256(convert_to(p_params,'UTF8')),'hex') IS DISTINCT FROM authority#>>'{params,sha256}' THEN RETURN false;END IF;
 SELECT outputs->0 INTO output FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task AND attempt=p_attempt AND fencing_token=p_fence;
 IF output IS NULL OR encode(sha256(convert_to(p_root,'UTF8')),'hex') IS DISTINCT FROM output->>'sha256' THEN RETURN false;END IF;
 root:=p_root::jsonb;report:=p_report::jsonb;params:=p_params::jsonb;
 IF root->'schema_version' IS DISTINCT FROM '1'::jsonb OR root->'bundle' IS DISTINCT FROM 'null'::jsonb
 OR root->'draft_shards' IS DISTINCT FROM report->'shards' OR report->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(report->'shards') IS DISTINCT FROM 'array' OR jsonb_array_length(report->'shards')<>jsonb_array_length(p_shards)
 OR coalesce((report->>'entry_count')::integer,-1) NOT BETWEEN 1 AND 5000
 OR jsonb_array_length(p_shards)<>((report->>'entry_count')::integer+99)/100
 OR encode(sha256(convert_to(p_report,'UTF8')),'hex') IS DISTINCT FROM root#>>'{report,sha256}' THEN RETURN false;END IF;
 IF photo.v30_interchange_import_document_scope(p_tenant,(authority->>'workflow_id')::uuid,
 (SELECT v30_import_parsed_jcs FROM photo.backend_documents WHERE tenant_id=p_tenant AND owner_id=p_task AND purpose='v30_interchange_import_evidence' AND state='ready'),
 (authority#>>'{params,artifact_id}')::uuid,'task_params',p_params) IS NULL THEN RETURN false;END IF;
 SELECT v30_import_parsed_jcs::jsonb INTO source FROM photo.backend_documents WHERE tenant_id=p_tenant AND owner_id=p_task AND purpose='v30_interchange_import_evidence' AND state='ready';
 IF root->'imported_files' IS DISTINCT FROM (SELECT jsonb_agg(value->'content' ORDER BY ord) FROM jsonb_array_elements(source->'files') WITH ORDINALITY f(value,ord)) THEN RETURN false;END IF;
 total:=octet_length(p_root)+octet_length(p_report);
 FOR shard IN SELECT value FROM jsonb_array_elements_text(p_shards) LOOP
  IF octet_length(shard) NOT BETWEEN 2 AND 8388608 THEN RETURN false;END IF;
  pin:=report->'shards'->ordinal;
  IF encode(sha256(convert_to(shard,'UTF8')),'hex') IS DISTINCT FROM pin->>'sha256'
   OR shard::jsonb->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(shard::jsonb->'entries') IS DISTINCT FROM 'array'
   OR jsonb_array_length(shard::jsonb->'entries')<>least(100,(report->>'entry_count')::integer-ordinal*100) THEN RETURN false;END IF;
  entries:=entries+jsonb_array_length(shard::jsonb->'entries');total:=total+octet_length(shard);ordinal:=ordinal+1;
 END LOOP;
 IF entries<>(report->>'entry_count')::integer OR total>(authority->>'max_output_bytes')::bigint THEN RETURN false;END IF;
 -- The existing adopter independently checks full reachable inventory, attempts,
 -- immutable versions, quota, and result identity in this same transaction.
 IF photo.adopt_v2_plan_pre_import(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(p_tenant,'v30-interchange-import',photo.v2_adoption_event_id(p_task,p_result),clock_timestamp());
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_interchange_import(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
