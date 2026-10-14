-- +goose Up
-- Serialize version allocation for repeated imports sharing the content-derived style ID.
-- Keep the deterministic version UUID for exact decision replay.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.register_v30_interchange_style(p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_revision bigint,p_report_sha text,
 p_parsed text,p_entry uuid,p_binding jsonb,p_style uuid,p_sha text,p_config text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
<<registration>>
DECLARE authority jsonb;d photo.backend_documents%ROWTYPE;b photo.backend_documents%ROWTYPE;
 config jsonb;style_id uuid;version_id uuid;project uuid;run uuid;bg uuid;original jsonb;mode text;metrics jsonb;next_version bigint;
BEGIN
 IF p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 8388608
 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR encode(sha256(convert_to(p_config,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN
 RAISE EXCEPTION 'INTERCHANGE_STYLE_CONFIG_INVALID' USING ERRCODE='23514';END IF;
 authority:=photo.v30_interchange_review_context(p_tenant,p_actor,p_session,p_workflow,p_revision,p_report_sha);
 IF authority IS NULL THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_STALE' USING ERRCODE='40001';END IF;
 project:=(authority->>'project_id')::uuid;run:=(authority->>'run_id')::uuid;
 PERFORM 1 FROM photo.v30_interchange_approval_document(p_tenant,p_actor,p_session,p_workflow,p_revision,p_report_sha,p_parsed,p_entry,p_binding,p_style,p_sha,'StyleV2');
 IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_STYLE_SOURCE_STALE' USING ERRCODE='40001';END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=run AND artifact_id=p_style
 AND owner_id=p_style AND purpose='v30_import_prepared' AND state='ready' FOR SHARE;
 original:=d.v30_import_prepared->'origin';config:=p_config::jsonb;
 style_id:=photo.v30_uuid5((p_binding->>'asset_version_id')::uuid,'interchange:style:'||(original->>'sha256'));
 version_id:=photo.v30_uuid5(p_workflow,'interchange:style-version:'||p_entry||':'||p_style);
 IF style_id IS NULL OR jsonb_typeof(config) IS DISTINCT FROM 'object' OR config->>'style_id' IS DISTINCT FROM style_id::text
 OR config#>>'{quality,status}' IS DISTINCT FROM 'needs_review'
 OR config#>>'{quality,transfer_confidence}' IS DISTINCT FROM 'unvalidated' THEN
 RAISE EXCEPTION 'INTERCHANGE_STYLE_IDENTITY_INVALID' USING ERRCODE='23514';END IF;
 mode:=config#>>'{extensions,background,mode}';
 IF mode='asset' THEN
  bg:=(config#>>'{extensions,background,asset_id}')::uuid;
  SELECT * INTO b FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=run AND artifact_id=bg AND owner_id=bg
  AND purpose='v30_import_prepared' AND state='ready' AND v30_import_prepared->>'contract'='StyleBackgroundAsset'
  AND v30_import_prepared->>'entry'=p_entry::text AND v30_import_prepared->'binding'=p_binding FOR SHARE;
  IF b.id IS NULL OR b.v30_import_prepared#>'{background_occurrence,style}' IS DISTINCT FROM
   jsonb_build_object('artifact_id',original->>'artifact_id','sha256',original->>'sha256') THEN
   RAISE EXCEPTION 'INTERCHANGE_STYLE_BACKGROUND_INVALID' USING ERRCODE='23514';END IF;
  PERFORM 1 FROM photo.v30_interchange_prepared_document(p_tenant,p_workflow,p_parsed,p_entry,p_binding,bg,b.sha256,'StyleBackgroundAsset');
  IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_STYLE_BACKGROUND_STALE' USING ERRCODE='40001';END IF;
 END IF;
 metrics:=jsonb_build_object('schema_version','interchange-style-approval/v1','workflow_id',p_workflow,'entry_id',p_entry,
 'original',original,'prepared',jsonb_build_object('artifact_id',p_style,'sha256',p_sha),'report_sha256',p_report_sha);
 INSERT INTO photo.styles(id,tenant_id,project_id,name,description,created_by)
 VALUES(style_id,p_tenant,project,'Imported '||style_id,'Imported through reviewed interchange',p_actor)
 ON CONFLICT(id) DO NOTHING;
 -- All import allocators for this identity serialize on its owning style row.
 PERFORM 1 FROM photo.styles s WHERE s.tenant_id=p_tenant AND s.id=registration.style_id
 AND s.project_id=project AND s.archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_STYLE_CONFLICT' USING ERRCODE='23514';END IF;
 SELECT v.version_no INTO next_version FROM photo.style_versions v
 WHERE v.tenant_id=p_tenant AND v.id=version_id AND v.style_id=registration.style_id;
 IF NOT FOUND THEN
  SELECT coalesce(max(v.version_no)::bigint,0)+1 INTO next_version FROM photo.style_versions v
  WHERE v.tenant_id=p_tenant AND v.style_id=registration.style_id;
 END IF;
 IF next_version NOT BETWEEN 1 AND 2147483647 THEN
 RAISE EXCEPTION 'INTERCHANGE_STYLE_VERSION_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.style_versions(id,tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,training_run_id,approval_status,metrics,export_artifact_id,source_file_sha256)
 VALUES(version_id,p_tenant,style_id,next_version::integer,config,p_sha,'2.0.0','photo_renderer_v2',run,'accepted',metrics,p_style,original->>'sha256')
 ON CONFLICT(id) DO NOTHING;
 IF NOT EXISTS(SELECT 1 FROM photo.style_versions v WHERE v.tenant_id=p_tenant AND v.id=version_id AND v.style_id=registration.style_id
 AND v.config_hash=p_sha AND v.config_json=config AND v.approval_status='accepted' AND v.export_artifact_id=p_style AND v.metrics=registration.metrics) THEN
 RAISE EXCEPTION 'INTERCHANGE_STYLE_VERSION_CONFLICT' USING ERRCODE='23514';END IF;
 IF mode='asset' THEN
  INSERT INTO photo.style_asset_bindings(tenant_id,style_version_id,portable_asset_id,artifact_id,sha256)
  VALUES(p_tenant,version_id,bg::text,bg,b.sha256) ON CONFLICT(tenant_id,style_version_id,portable_asset_id) DO NOTHING;
  IF NOT EXISTS(SELECT 1 FROM photo.style_asset_bindings a WHERE a.tenant_id=p_tenant AND a.style_version_id=version_id
  AND a.portable_asset_id=bg::text AND a.artifact_id=bg AND a.sha256=b.sha256) THEN
  RAISE EXCEPTION 'INTERCHANGE_STYLE_ASSET_CONFLICT' USING ERRCODE='23514';END IF;
 END IF;
 RETURN version_id;
END $$;
ALTER FUNCTION photo.register_v30_interchange_style(uuid,uuid,uuid,uuid,bigint,text,text,uuid,jsonb,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v30_interchange_style(uuid,uuid,uuid,uuid,bigint,text,text,uuid,jsonb,uuid,text,text) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
