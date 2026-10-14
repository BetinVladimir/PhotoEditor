-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v30_interchange_ingest(p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_parsed uuid,p_sha text,p_body text,p_items text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;parent photo.runs%ROWTYPE;r photo.runs%ROWTYPE;sn photo.snapshots%ROWTYPE;
 rid uuid;sid uuid;body jsonb;items jsonb;expected jsonb;config jsonb;digest text;item jsonb;entry jsonb;file jsonb;a photo.artifacts%ROWTYPE;n integer:=0;ord integer:=0;suffix text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
 OR p_items IS NULL OR octet_length(p_items) NOT BETWEEN 2 AND 8388608 OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange' FOR SHARE;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id;
 IF w.id IS NULL OR parent.created_by IS DISTINCT FROM p_actor OR parent.cancel_requested
 OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id AND archived_at IS NULL AND download_allowed) THEN RETURN false;END IF;
 PERFORM a.id FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 WHERE a.tenant_id=p_tenant AND a.id=p_parsed AND a.sha256=p_sha AND a.bytes=octet_length(p_body) AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.output_role='document' AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND t.run_id=w.run_id AND t.operation='v2.interchange.parse' AND t.state='succeeded'
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant AND l.workflow_id=p_workflow AND l.artifact_id=a.id AND l.sha256=a.sha256) FOR SHARE OF a,t;
 IF NOT FOUND THEN RETURN false;END IF;
 body:=p_body::jsonb;items:=p_items::jsonb;expected:='[]'::jsonb;
 IF body->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(body->'manifest'->'entries') IS DISTINCT FROM 'array'
 OR jsonb_array_length(body->'manifest'->'entries') NOT BETWEEN 1 AND 5000 OR jsonb_typeof(items) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(body->'manifest'->'entries') LOOP
  SELECT value INTO file FROM jsonb_array_elements(body->'files') WHERE value->>'path'=entry->>'image_path';
  IF file IS NULL OR file->'content'->>'sha256' IS DISTINCT FROM entry->>'image_sha256'
  OR (SELECT count(*) FROM jsonb_array_elements(body->'files') WHERE value->>'path'=entry->>'image_path')<>1 THEN RETURN false;END IF;
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(file->'content'->>'artifact_id')::uuid AND sha256=entry->>'image_sha256'
  AND state='ready' AND verified_at IS NOT NULL AND media_type='application/octet-stream' AND output_role='imported_file'
  AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
  IF a.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant AND l.workflow_id=p_workflow AND l.artifact_id=a.id AND l.sha256=a.sha256) THEN RETURN false;END IF;
  suffix:=lower(substring(entry->>'image_path' FROM '\.[^./]+$'));
  IF a.bytes BETWEEN 1 AND 536870912 AND suffix=ANY(ARRAY['.jpg','.jpeg','.png','.tif','.tiff','.nef','.nrw','.crw','.cr2','.cr3','.arw','.srf','.sr2','.mrw','.dng','.rw2','.orf','.raf','.pef','.srw','.3fr','.fff','.iiq','.mos','.kdc','.mef','.x3f']) THEN
   expected:=expected||jsonb_build_array(jsonb_build_object('ordinal',ord,'artifact_id',a.id,'sha256',a.sha256,'bytes',a.bytes,
    'payload',jsonb_build_object('schema_version',1,'suffix',suffix,'assume_profile',NULL,'analyze',false,'tile',512)));n:=n+1;
  END IF;
  ord:=ord+1;
 END LOOP;
 IF n=0 OR items IS DISTINCT FROM expected THEN RETURN false;END IF;
 rid:=photo.v30_uuid5(p_workflow,'interchange:canonical-ingest:1');sid:=photo.v30_uuid5(rid,'snapshot');digest:=encode(sha256(convert_to(p_items,'UTF8')),'hex');
 config:=jsonb_build_object('schema_version',1,'interchange_workflow_id',p_workflow,'parsed',jsonb_build_object('artifact_id',p_parsed,'sha256',p_sha),'snapshot_digest',digest);
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 IF FOUND THEN
  SELECT * INTO sn FROM photo.snapshots WHERE tenant_id=p_tenant AND id=sid;
  RETURN r.kind='ingest' AND r.created_by=p_actor AND NOT r.cancel_requested AND r.input_snapshot_id=sid AND r.config_snapshot=config
  AND sn.digest=digest AND sn.item_count=n AND sn.principal_type='user' AND sn.principal_id=p_actor;
 END IF;
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count)
 VALUES(sid,p_tenant,'ingest','user',p_actor,digest,n);
 FOR item IN SELECT value FROM jsonb_array_elements(items) LOOP
  INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload)
  VALUES(p_tenant,sid,(item->>'ordinal')::bigint,(item->>'artifact_id')::uuid,item->>'sha256',item->'payload');
 END LOOP;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(rid,p_tenant,'ingest','queued',sid,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),p_actor,'continue_items',false);
 FOR item IN SELECT value FROM jsonb_array_elements(items) LOOP
  PERFORM asset_id FROM photo.bind_v30_interchange_ingest(p_tenant,p_actor,p_session,rid,p_workflow,p_parsed,p_sha,p_body,(item->>'ordinal')::integer);
  IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_INGEST_BINDING_REFUSED' USING ERRCODE='23514';END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.begin_v30_interchange_ingest(uuid,uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v30_interchange_ingest(uuid,uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.begin_v30_interchange_ingest(uuid,uuid,uuid,uuid,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
