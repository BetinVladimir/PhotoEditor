-- +goose Up
CREATE TABLE photo.ingest_catalog_items (
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,ordinal bigint NOT NULL,
 asset_id uuid NOT NULL,version_id uuid NOT NULL,original_id uuid NOT NULL,master_id uuid NOT NULL,master_sha text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,run_id,ordinal),
 FOREIGN KEY(tenant_id,run_id,ordinal) REFERENCES photo.ingest_upload_bindings(tenant_id,run_id,ordinal),
 FOREIGN KEY(tenant_id,asset_id) REFERENCES photo.assets(tenant_id,id),
 FOREIGN KEY(tenant_id,version_id) REFERENCES photo.asset_versions(tenant_id,id),
 FOREIGN KEY(tenant_id,original_id) REFERENCES photo.artifacts(tenant_id,id),
 FOREIGN KEY(tenant_id,master_id) REFERENCES photo.artifacts(tenant_id,id),
 CHECK(master_sha ~ '^[0-9a-f]{64}$')
);
ALTER TABLE photo.ingest_catalog_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.ingest_catalog_items FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.ingest_catalog_items TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.ingest_catalog_items TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.register_ingest_asset(p_tenant uuid,p_run uuid,p_ordinal bigint,p_master uuid,p_sha text,p_image jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;b photo.ingest_upload_bindings%ROWTYPE;d photo.ingest_originals%ROWTYPE;
 u photo.upload_sessions%ROWTYPE;old photo.ingest_catalog_items%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 master photo.artifacts%ROWTYPE;ref jsonb;role_name text;review_status text;
BEGIN
 IF p_master IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_image IS NULL OR jsonb_typeof(p_image) IS DISTINCT FROM 'object'
 OR octet_length(p_image::text)>1048576 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR NOT r.graph_sealed OR r.status NOT IN ('queued','running')
 OR NOT EXISTS(SELECT FROM photo.ingest_progress p WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND p.phase IN ('decode','catalog')) THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.ingest_upload_bindings x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.ingest_catalog_items x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal;
 IF FOUND THEN RETURN old.master_id=p_master AND old.master_sha=p_sha AND old.asset_id=b.asset_id AND old.version_id=b.version_id;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=b.project_id AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=p_tenant AND id=b.upload_id FOR UPDATE;
 IF NOT FOUND OR u.state<>'verifying' OR u.staging_artifact_id IS DISTINCT FROM b.source_id OR u.project_id<>b.project_id
 OR u.collection_id IS DISTINCT FROM b.collection_id OR u.principal_type<>b.principal_type OR u.principal_id<>b.principal_id OR u.verified_version_id IS NOT NULL THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.ingest_originals x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal;
 IF NOT FOUND OR d.state<>'ready' OR d.source_id<>b.source_id OR d.source_sha<>b.source_sha
 OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.source_sha AND a.state='ready' AND a.object_version=d.object_version) THEN RETURN false;END IF;
 SELECT task.* INTO t FROM photo.ingest_node_plans n JOIN photo.tasks task ON task.tenant_id=n.tenant_id AND task.run_id=n.run_id AND task.id=n.id
 WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.ordinal=p_ordinal AND n.operation='asset.decode' AND n.source_artifact_id=b.source_id AND n.source_sha=b.source_sha
 AND task.operation='asset.decode' AND task.state='succeeded';
 IF NOT FOUND OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=t.canonical_result_id;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO master FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_master AND sha256=p_sha AND state='ready' AND created_by_task_id=t.id AND output_role='working_image'
 AND media_type='application/json' AND (expires_at IS NULL OR expires_at>clock_timestamp());
 IF NOT FOUND OR NOT(z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_master::text,'sha256',p_sha)))
 OR p_image->'canvas'->>'source_sha256' IS DISTINCT FROM b.source_sha OR p_image->>'dtype' IS DISTINCT FROM 'float32'
 OR p_image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1' THEN RETURN false;END IF;
 FOR role_name,ref IN SELECT 'working_rgb',p_image->'pixels' UNION ALL SELECT 'alpha',p_image->'alpha' WHERE p_image->'alpha'<>'null'::jsonb LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT(z.outputs @> jsonb_build_array(ref))
  OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id::text=ref->>'artifact_id' AND a.sha256=ref->>'sha256'
   AND a.state='ready' AND a.created_by_task_id=t.id AND a.output_role=role_name AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 review_status:=CASE WHEN b.principal_type='user' THEN 'accepted' ELSE 'pending_review' END;
 INSERT INTO photo.assets(id,tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance)
 VALUES(b.asset_id,p_tenant,b.project_id,d.artifact_id,p_image,'verified',jsonb_build_object('kind','upload','upload_id',b.upload_id,'run_id',p_run,'ordinal',p_ordinal,'source_sha256',b.source_sha));
 INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,created_by_run_id,content_hash,metadata,review_state,version_no)
 VALUES(b.version_id,p_tenant,b.asset_id,p_master,p_run,p_sha,p_image,review_status,1);
 INSERT INTO photo.ingest_catalog_items(tenant_id,run_id,ordinal,asset_id,version_id,original_id,master_id,master_sha)
 VALUES(p_tenant,p_run,p_ordinal,b.asset_id,b.version_id,d.artifact_id,p_master,p_sha);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_ingest_asset(uuid,uuid,bigint,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_ingest_asset(uuid,uuid,bigint,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_ingest_asset(uuid,uuid,bigint,uuid,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
