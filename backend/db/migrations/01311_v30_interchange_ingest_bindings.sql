-- +goose Up
-- Imported-source identity is separate from upload-session identity.
-- +goose StatementBegin
CREATE TABLE photo.v30_interchange_ingest_bindings(
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,ordinal integer NOT NULL CHECK(ordinal BETWEEN 0 AND 4999),
 workflow_id uuid NOT NULL,project_id uuid NOT NULL,actor_id uuid NOT NULL,
 parsed_id uuid NOT NULL,parsed_sha text NOT NULL CHECK(parsed_sha ~ '^[0-9a-f]{64}$'),
 entry_id uuid NOT NULL,external_id text NOT NULL CHECK(length(external_id) BETWEEN 1 AND 240),
 source_id uuid NOT NULL,source_sha text NOT NULL CHECK(source_sha ~ '^[0-9a-f]{64}$'),
 asset_id uuid NOT NULL,version_id uuid NOT NULL,item_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,run_id,ordinal),UNIQUE(tenant_id,workflow_id,entry_id),UNIQUE(asset_id),UNIQUE(version_id),UNIQUE(item_id),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 FOREIGN KEY(tenant_id,parsed_id) REFERENCES photo.artifacts(tenant_id,id),
 FOREIGN KEY(tenant_id,source_id) REFERENCES photo.artifacts(tenant_id,id)
);
ALTER TABLE photo.v30_interchange_ingest_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_interchange_ingest_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_interchange_ingest_bindings TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v30_interchange_ingest_bindings FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT SELECT,INSERT ON photo.v30_interchange_ingest_bindings TO photo_fn_owner;
CREATE TRIGGER v30_interchange_ingest_bindings_immutable BEFORE UPDATE ON photo.v30_interchange_ingest_bindings
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();

CREATE FUNCTION photo.bind_v30_interchange_ingest(p_tenant uuid,p_actor uuid,p_session uuid,p_run uuid,p_workflow uuid,p_parsed uuid,p_sha text,p_body text,p_ordinal integer)
RETURNS TABLE(asset_id uuid,version_id uuid,item_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;parent photo.runs%ROWTYPE;
 old photo.v30_interchange_ingest_bindings%ROWTYPE;body jsonb;entry jsonb;file jsonb;source uuid;source_sha text;aid uuid;vid uuid;iid uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
 OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange' FOR SHARE;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id;
 IF w.id IS NULL OR parent.created_by IS DISTINCT FROM p_actor OR parent.cancel_requested
 OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id AND archived_at IS NULL AND download_allowed)
 OR p_run IS DISTINCT FROM photo.v30_uuid5(p_workflow,'interchange:canonical-ingest:1') THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'ingest' OR r.created_by IS DISTINCT FROM p_actor OR r.cancel_requested
 OR r.status NOT IN('draft','queued','running') OR r.config_snapshot->>'interchange_workflow_id' IS DISTINCT FROM p_workflow::text THEN RETURN;END IF;
 PERFORM a.id FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 WHERE a.tenant_id=p_tenant AND a.id=p_parsed AND a.sha256=p_sha AND a.bytes=octet_length(p_body)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.output_role='document'
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND t.run_id=w.run_id AND t.operation='v2.interchange.parse' AND t.state='succeeded'
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant AND l.workflow_id=p_workflow AND l.artifact_id=a.id AND l.sha256=a.sha256) FOR SHARE OF a,t;
 IF NOT FOUND THEN RETURN;END IF;
 body:=p_body::jsonb;entry:=body->'manifest'->'entries'->p_ordinal;
 IF body->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(entry) IS DISTINCT FROM 'object' THEN RETURN;END IF;
 SELECT value INTO file FROM jsonb_array_elements(body->'files') WHERE value->>'path'=entry->>'image_path';
 IF file IS NULL OR file->'content'->>'sha256' IS DISTINCT FROM entry->>'image_sha256'
 OR (SELECT count(*) FROM jsonb_array_elements(body->'files') WHERE value->>'path'=entry->>'image_path')<>1 THEN RETURN;END IF;
 source:=(file->'content'->>'artifact_id')::uuid;source_sha:=file->'content'->>'sha256';
 PERFORM a.id FROM photo.artifacts a JOIN photo.snapshot_items si ON si.tenant_id=a.tenant_id AND si.artifact_id=a.id AND si.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND a.id=source AND a.sha256=source_sha AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/octet-stream' AND a.output_role='imported_file' AND a.bytes BETWEEN 1 AND 536870912
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND si.snapshot_id=r.input_snapshot_id AND si.ordinal=p_ordinal
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant AND l.workflow_id=p_workflow AND l.artifact_id=a.id AND l.sha256=a.sha256) FOR SHARE OF a,si;
 IF NOT FOUND THEN RETURN;END IF;
 aid:=photo.v30_uuid5(p_run,'asset:'||p_ordinal);vid:=photo.v30_uuid5(p_run,'version:'||p_ordinal);iid:=photo.v30_uuid5(p_run,'item:'||p_ordinal);
 SELECT * INTO old FROM photo.v30_interchange_ingest_bindings b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.ordinal=p_ordinal;
 IF FOUND THEN
  IF old.workflow_id=p_workflow AND old.parsed_id=p_parsed AND old.parsed_sha=p_sha AND old.source_id=source AND old.source_sha=source_sha
  AND old.entry_id=(entry->>'entry_id')::uuid AND old.external_id=entry->>'external_id' THEN RETURN QUERY SELECT old.asset_id,old.version_id,old.item_id;END IF;
  RETURN;
 END IF;
 IF r.graph_sealed OR EXISTS(SELECT 1 FROM photo.ingest_progress WHERE tenant_id=p_tenant AND run_id=p_run) THEN RETURN;END IF;
 INSERT INTO photo.v30_interchange_ingest_bindings(tenant_id,run_id,ordinal,workflow_id,project_id,actor_id,parsed_id,parsed_sha,entry_id,external_id,source_id,source_sha,asset_id,version_id,item_id)
 VALUES(p_tenant,p_run,p_ordinal,p_workflow,w.project_id,p_actor,p_parsed,p_sha,(entry->>'entry_id')::uuid,entry->>'external_id',source,source_sha,aid,vid,iid);
 RETURN QUERY SELECT aid,vid,iid;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.bind_v30_interchange_ingest(uuid,uuid,uuid,uuid,uuid,uuid,text,text,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_v30_interchange_ingest(uuid,uuid,uuid,uuid,uuid,uuid,text,text,integer) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.bind_v30_interchange_ingest(uuid,uuid,uuid,uuid,uuid,uuid,text,text,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
