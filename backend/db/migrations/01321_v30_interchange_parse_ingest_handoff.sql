-- +goose Up
-- Parsed links and queued canonical ingest appear together, or neither appears.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v30_interchange_parsed_and_begin_ingest(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text,p_params text,p_body text,p_items text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;actor uuid;session uuid;root jsonb;items jsonb;body jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_items IS NULL OR octet_length(p_items) NOT BETWEEN 2 AND 8388608 THEN RETURN false;END IF;
 items:=p_items::jsonb;
 IF jsonb_typeof(items) IS DISTINCT FROM 'array' OR jsonb_array_length(items)>5000 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT created_by INTO actor FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=t.run_id AND workflow_type='interchange';
 SELECT (d.v30_interchange_proof->>'auth_session_id')::uuid INTO session FROM photo.backend_documents d
 WHERE d.tenant_id=p_tenant AND d.run_id=t.run_id AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready';
 IF actor IS NULL OR session IS NULL OR w.id IS NULL THEN RETURN false;END IF;
 IF photo.adopt_v30_interchange_parsed(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest,p_params,p_body) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT outputs->0 INTO root FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task;
 IF jsonb_array_length(items)>0 THEN
  IF photo.begin_v30_interchange_ingest(p_tenant,actor,session,w.id,(root->>'artifact_id')::uuid,root->>'sha256',p_body,p_items) IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'INTERCHANGE_INGEST_HANDOFF_REFUSED' USING ERRCODE='23514';
  END IF;
 ELSE
  -- All unsupported entries still proceed to mapping/report; never fabricate
  -- an empty ingest run or silently drop a supported source.
  body:=p_body::jsonb;
  IF EXISTS(SELECT 1 FROM jsonb_array_elements(body->'manifest'->'entries') e
   JOIN LATERAL jsonb_array_elements(body->'files') f ON f->>'path'=e->>'image_path'
   JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(f#>>'{content,artifact_id}')::uuid AND a.sha256=e->>'image_sha256'
   WHERE a.bytes BETWEEN 1 AND 536870912 AND lower(substring(e->>'image_path' FROM '\.[^./]+$'))=ANY(ARRAY['.jpg','.jpeg','.png','.tif','.tiff','.nef','.nrw','.crw','.cr2','.cr3','.arw','.srf','.sr2','.mrw','.dng','.rw2','.orf','.raf','.pef','.srw','.3fr','.fff','.iiq','.mos','.kdc','.mef','.x3f'])) THEN
   RAISE EXCEPTION 'INTERCHANGE_INGEST_HANDOFF_EMPTY' USING ERRCODE='23514';
  END IF;
 END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(p_tenant,'v30-interchange-ingest-started',photo.v2_adoption_event_id(p_task,p_result),clock_timestamp());
 RETURN true;
END $$;
ALTER FUNCTION photo.adopt_v30_interchange_parsed_and_begin_ingest(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_interchange_parsed_and_begin_ingest(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_interchange_parsed_and_begin_ingest(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text) TO photo_scheduler;
-- Low-level parse commit is only callable by the owning handoff function.
REVOKE EXECUTE ON FUNCTION photo.adopt_v30_interchange_parsed(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text) FROM photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
