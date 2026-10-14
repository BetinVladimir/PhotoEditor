-- +goose Up
-- Opaque parsed files never acquire recipe/mask/approval authority here.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_interchange;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_interchange(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='v2.interchange.parse') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_interchange(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
CREATE FUNCTION photo.adopt_v30_interchange_parsed(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text,p_params text,p_body text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;params_ref record;root jsonb;body jsonb;params jsonb;file jsonb;expected jsonb;ref jsonb;ordinal integer:=0;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO params_ref FROM photo.v30_interchange_parse_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result);
 IF NOT FOUND OR encode(sha256(convert_to(p_params,'UTF8')),'hex') IS DISTINCT FROM params_ref.sha256 THEN RETURN false;END IF;
 SELECT outputs->0 INTO root FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task AND attempt=p_attempt AND fencing_token=p_fence;
 IF root IS NULL OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM root->>'sha256' THEN RETURN false;END IF;
 params:=p_params::jsonb;body:=p_body::jsonb;
 IF body->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(body->'files') IS DISTINCT FROM 'array'
 OR jsonb_array_length(body->'files') NOT BETWEEN 1 AND 30000 OR body->'manifest'->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR jsonb_typeof(body->'manifest'->'inventory') IS DISTINCT FROM 'array'
 OR jsonb_array_length(body->'files')<>jsonb_array_length(body->'manifest'->'inventory') THEN RETURN false;END IF;
 -- Current source readiness is rechecked inside the atomic commit.
 PERFORM a.id FROM photo.artifacts a JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='InterchangeArchive'
 AND a.id=(params#>>'{request,bundle,artifact_id}')::uuid AND a.sha256=params#>>'{request,bundle,sha256}'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/zip'
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 IF NOT FOUND THEN RETURN false;END IF;
 expected:=jsonb_build_array(jsonb_build_object('artifact_id',root->>'artifact_id','sha256',root->>'sha256','ordinal',0,
 'purpose',encode(sha256(convert_to('tasks/'||p_task::text||'/$','UTF8')),'hex')));
 FOR file IN SELECT value FROM jsonb_array_elements(body->'files') LOOP
  IF (SELECT count(*) FROM jsonb_array_elements(body->'manifest'->'inventory') inv WHERE inv->>'path'=file->>'path'
   AND inv->>'sha256'=file#>>'{content,sha256}' AND inv->>'contract'=file->>'contract')<>1 THEN RETURN false;END IF;
  ref:=file->'content';ordinal:=ordinal+1;
  expected:=expected||jsonb_build_array(jsonb_build_object('artifact_id',ref->>'artifact_id','sha256',ref->>'sha256','ordinal',ordinal,
   'purpose',encode(sha256(convert_to('tasks/'||p_task::text||'/$/files/'||(ordinal-1)::text||'/content','UTF8')),'hex')));
 END LOOP;
 IF p_inventory IS DISTINCT FROM expected OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(expected))<>jsonb_array_length(expected) THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(expected) LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(ref->>'artifact_id')::uuid FOR SHARE;
  IF a.id IS NULL OR a.sha256 IS DISTINCT FROM ref->>'sha256' OR a.created_by_task_id IS DISTINCT FROM p_task
  OR a.produced_attempt IS DISTINCT FROM p_attempt OR a.produced_fence IS DISTINCT FROM p_fence
  OR ((ref->>'ordinal')::integer=0 AND (a.media_type<>'application/json' OR a.output_role<>'document' OR a.bytes<>octet_length(p_body)))
  OR ((ref->>'ordinal')::integer>0 AND (a.media_type<>'application/octet-stream' OR a.output_role<>'imported_file')) THEN RETURN false;END IF;
 END LOOP;
 IF photo.adopt_v2_plan_pre_interchange(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(p_tenant,'v30-interchange-parsed',photo.v2_adoption_event_id(p_task,p_result),clock_timestamp());
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v30_interchange_parsed(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_interchange_parsed(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_interchange_parsed(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
