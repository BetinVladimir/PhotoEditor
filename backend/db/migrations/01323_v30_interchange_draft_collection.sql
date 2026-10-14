-- +goose Up
-- Import owns a new draft collection, never updates existing project items.
-- +goose StatementBegin
CREATE FUNCTION photo.prepare_v30_interchange_draft_collection(p_tenant uuid,p_workflow uuid,p_body text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;body jsonb;row jsonb;entry jsonb;w photo.v2_workflows%ROWTYPE;c photo.collections%ROWTYPE;i photo.collection_items%ROWTYPE;
 cid uuid;iid uuid;aid uuid;vid uuid;rating integer;flag text;position bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange';
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR UPDATE;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_body);
 IF sources IS NULL THEN RETURN NULL;END IF;
 body:=p_body::jsonb;cid:=photo.v30_uuid5(p_workflow,'interchange:draft-collection:1');
 SELECT * INTO c FROM photo.collections WHERE tenant_id=p_tenant AND id=cid FOR UPDATE;
 IF NOT FOUND THEN
  INSERT INTO photo.collections(id,tenant_id,project_id,name,state,source_run_id,sort_mode)
  VALUES(cid,p_tenant,w.project_id,'Imported photos','draft',w.run_id,'manual');
 ELSE
  IF c.project_id IS DISTINCT FROM w.project_id OR c.source_run_id IS DISTINCT FROM w.run_id OR c.state<>'draft' OR c.deleted_at IS NOT NULL THEN
   RAISE EXCEPTION 'INTERCHANGE_COLLECTION_CONFLICT' USING ERRCODE='23514';
  END IF;
 END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(sources->'entries') LOOP
  IF row->>'state'<>'adopted' THEN CONTINUE;END IF;
  entry:=body->'manifest'->'entries'->((row->>'ordinal')::integer);
  vid:=(row->>'asset_version_id')::uuid;
  SELECT asset_id INTO aid FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=vid;
  iid:=photo.v30_uuid5(photo.v30_uuid5(p_workflow,'interchange:canonical-ingest:1'),'item:'||(row->>'ordinal'));
  -- -1 has no local rating equivalent: preserve it in immutable parsed metadata
  -- and report unsupported; the local draft starts at its neutral rating 0.
  rating:=greatest(0,(entry->>'rating')::integer);
  flag:=CASE entry->>'pick' WHEN 'picked' THEN 'pick' WHEN 'rejected' THEN 'reject' WHEN 'unmarked' THEN 'none' ELSE NULL END;
  position:=(row->>'ordinal')::bigint*1024;
  IF aid IS NULL OR flag IS NULL OR rating NOT BETWEEN 0 AND 5 THEN RAISE EXCEPTION 'INTERCHANGE_COLLECTION_SOURCE_INVALID' USING ERRCODE='23514';END IF;
  SELECT * INTO i FROM photo.collection_items WHERE tenant_id=p_tenant AND id=iid FOR UPDATE;
  IF NOT FOUND THEN
   INSERT INTO photo.collection_items(id,tenant_id,collection_id,asset_id,asset_version_id,position,visibility,moderation_state,rating,flag,created_by_run_id)
   VALUES(iid,p_tenant,cid,aid,vid,position,'hidden','pending_review',rating,flag,w.run_id);
  ELSE
   IF i.collection_id IS DISTINCT FROM cid OR i.asset_id IS DISTINCT FROM aid OR i.asset_version_id IS DISTINCT FROM vid
   OR i.created_by_run_id IS DISTINCT FROM w.run_id OR i.position IS DISTINCT FROM position OR i.rating IS DISTINCT FROM rating OR i.flag IS DISTINCT FROM flag THEN
    RAISE EXCEPTION 'INTERCHANGE_COLLECTION_ITEM_CONFLICT' USING ERRCODE='23514';
   END IF;
  END IF;
 END LOOP;
 IF (SELECT count(*) FROM photo.collection_items WHERE tenant_id=p_tenant AND collection_id=cid)
 <> (SELECT count(*) FROM jsonb_array_elements(sources->'entries') x WHERE x->>'state'='adopted') THEN
  RAISE EXCEPTION 'INTERCHANGE_COLLECTION_COUNT_CONFLICT' USING ERRCODE='23514';
 END IF;
 RETURN cid;
END $$;
ALTER FUNCTION photo.prepare_v30_interchange_draft_collection(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.prepare_v30_interchange_draft_collection(uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.prepare_v30_interchange_draft_collection(uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
