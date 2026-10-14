-- +goose Up
-- Current local metadata evidence, scoped to the new import collection only.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_metadata_receipts(p_tenant uuid,p_workflow uuid,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;c photo.collections%ROWTYPE;w photo.v2_workflows%ROWTYPE;i photo.collection_items%ROWTYPE;
 row jsonb;entries jsonb:='[]'::jsonb;cid uuid;iid uuid;
BEGIN
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_body);IF sources IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 cid:=photo.v30_uuid5(p_workflow,'interchange:draft-collection:1');
 SELECT * INTO c FROM photo.collections WHERE tenant_id=p_tenant AND id=cid FOR SHARE;
 IF c.id IS NULL OR c.project_id IS DISTINCT FROM w.project_id OR c.source_run_id IS DISTINCT FROM w.run_id OR c.state<>'draft' OR c.deleted_at IS NOT NULL THEN RETURN NULL;END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(sources->'entries') LOOP
  IF row->>'state'<>'adopted' THEN CONTINUE;END IF;
  iid:=photo.v30_uuid5(photo.v30_uuid5(p_workflow,'interchange:canonical-ingest:1'),'item:'||(row->>'ordinal'));
  SELECT * INTO i FROM photo.collection_items WHERE tenant_id=p_tenant AND id=iid AND collection_id=cid
  AND asset_version_id=(row->>'asset_version_id')::uuid AND created_by_run_id=w.run_id FOR SHARE;
  IF i.id IS NULL THEN RETURN NULL;END IF;
  entries:=entries||jsonb_build_array(jsonb_build_object('entry_id',row->>'entry_id','asset_version_id',i.asset_version_id,
   'master',row->'master','item_id',i.id,'rating',i.rating,'flag',i.flag));
 END LOOP;
 IF (SELECT count(*) FROM photo.collection_items WHERE tenant_id=p_tenant AND collection_id=cid)<>jsonb_array_length(entries) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'workflow_id',w.id,'project_id',w.project_id,'parsed',sources->'parsed','collection_id',cid,'entries',entries);
END $$;
ALTER FUNCTION photo.v30_interchange_metadata_receipts(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_metadata_receipts(uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_metadata_receipts(uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
