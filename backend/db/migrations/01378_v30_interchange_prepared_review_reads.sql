-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_interchange_import_sources(p_tenant uuid,p_workflow uuid,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;child photo.runs%ROWTYPE;
 t photo.tasks%ROWTYPE;b photo.v30_interchange_ingest_bindings%ROWTYPE;o photo.v30_interchange_ingest_outcomes%ROWTYPE;
 a photo.artifacts%ROWTYPE;v photo.asset_versions%ROWTYPE;session uuid;parsed jsonb;body jsonb;entry jsonb;file jsonb;rows jsonb:='[]'::jsonb;ord integer:=0;n integer:=0;reason text;suffix text;rid uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange' AND renderer_version='v2.2';
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR SHARE;
 IF r.id IS NULL OR r.status NOT IN('running','waiting_review') OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN NULL;END IF;
 -- Review may reread existing prepared bytes only after the canonical import
 -- result was atomically adopted. Reservation/complete retain running-only gates.
 IF r.status='waiting_review' AND NOT EXISTS(
  SELECT 1 FROM photo.tasks reviewed WHERE reviewed.tenant_id=p_tenant AND reviewed.run_id=r.id
  AND reviewed.id=photo.v30_uuid5(p_workflow,'v2.interchange.import:0')
  AND reviewed.operation='v2.interchange.import' AND reviewed.operation_version='1'
  AND reviewed.state='succeeded' AND NOT reviewed.cancel_requested
  AND photo.v30_interchange_import_adoption_committed(p_tenant,reviewed.id) IS TRUE
 ) THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND operation='v2.interchange.parse' AND operation_version='1' AND state='succeeded' AND NOT cancel_requested FOR SHARE;
 IF t.id IS NULL OR photo.v30_interchange_parse_adoption_committed(p_tenant,t.id) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT (d.v30_interchange_proof->>'auth_session_id')::uuid INTO session FROM photo.backend_documents d
 WHERE d.tenant_id=p_tenant AND d.run_id=r.id AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready';
 IF photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id AND archived_at IS NULL AND download_allowed) THEN RETURN NULL;END IF;
 SELECT outputs->0 INTO parsed FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id;
 IF encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM parsed->>'sha256' THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(parsed->>'artifact_id')::uuid AND sha256=parsed->>'sha256' AND bytes=octet_length(p_body)
 AND state='ready' AND verified_at IS NOT NULL AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 body:=p_body::jsonb;rid:=photo.v30_uuid5(p_workflow,'interchange:canonical-ingest:1');
 SELECT * INTO child FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR SHARE;
 FOR entry IN SELECT value FROM jsonb_array_elements(body->'manifest'->'entries') LOOP
  SELECT value INTO file FROM jsonb_array_elements(body->'files') WHERE value->>'path'=entry->>'image_path';
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(file#>>'{content,artifact_id}')::uuid AND sha256=entry->>'image_sha256'
  AND state='ready' AND verified_at IS NOT NULL AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
  IF a.id IS NULL THEN RETURN NULL;END IF;
  suffix:=lower(substring(entry->>'image_path' FROM '\.[^./]+$'));reason:=NULL;
  IF suffix IS NULL OR suffix<>ALL(ARRAY['.jpg','.jpeg','.png','.tif','.tiff','.nef','.nrw','.crw','.cr2','.cr3','.arw','.srf','.sr2','.mrw','.dng','.rw2','.orf','.raf','.pef','.srw','.3fr','.fff','.iiq','.mos','.kdc','.mef','.x3f']) THEN reason:='unsupported_image_format';
  ELSIF a.bytes>536870912 THEN reason:='ingest_file_limit';END IF;
  IF reason IS NOT NULL THEN
   rows:=rows||jsonb_build_array(jsonb_build_object('ordinal',ord,'entry_id',entry->>'entry_id','state','unsupported','reason',reason,'asset_version_id',NULL,'master',NULL));
  ELSE
   n:=n+1;
   SELECT * INTO b FROM photo.v30_interchange_ingest_bindings WHERE tenant_id=p_tenant AND run_id=rid AND ordinal=ord;
   SELECT * INTO o FROM photo.v30_interchange_ingest_outcomes WHERE tenant_id=p_tenant AND run_id=rid AND ordinal=ord;
   IF b.run_id IS NULL OR o.run_id IS NULL OR b.workflow_id<>w.id OR b.entry_id IS DISTINCT FROM (entry->>'entry_id')::uuid
   OR b.source_id IS DISTINCT FROM a.id OR b.source_sha IS DISTINCT FROM a.sha256 THEN RETURN NULL;END IF;
   IF o.state='adopted' THEN
    SELECT * INTO v FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=b.version_id AND asset_id=b.asset_id AND created_by_run_id=rid AND master_artifact_id=o.master_id AND content_hash=o.master_sha;
    IF v.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=o.master_id AND sha256=o.master_sha AND state='ready' AND verified_at IS NOT NULL AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
    rows:=rows||jsonb_build_array(jsonb_build_object('ordinal',ord,'entry_id',entry->>'entry_id','state','adopted','reason',NULL,'asset_version_id',b.version_id,'master',jsonb_build_object('artifact_id',o.master_id,'sha256',o.master_sha)));
   ELSE
    rows:=rows||jsonb_build_array(jsonb_build_object('ordinal',ord,'entry_id',entry->>'entry_id','state','rejected','reason',o.failure_stage||':'||o.failure_reason,'asset_version_id',NULL,'master',NULL));
   END IF;
  END IF;
  ord:=ord+1;
 END LOOP;
 IF n>0 AND (child.id IS NULL OR child.kind<>'ingest' OR child.cancel_requested OR child.status NOT IN('completed','completed_with_errors','failed')
 OR NOT EXISTS(SELECT 1 FROM photo.ingest_progress WHERE tenant_id=p_tenant AND run_id=rid AND phase='complete')
 OR (SELECT count(*) FROM photo.v30_interchange_ingest_bindings WHERE tenant_id=p_tenant AND run_id=rid)<>n
 OR (SELECT count(*) FROM photo.v30_interchange_ingest_outcomes WHERE tenant_id=p_tenant AND run_id=rid)<>n) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'workflow_id',w.id,'project_id',w.project_id,'parsed',parsed,'ingest_run_id',CASE WHEN n>0 THEN rid ELSE NULL END,'entries',rows);
END $$;
ALTER FUNCTION photo.v30_interchange_import_sources(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_sources(uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_import_sources(uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
