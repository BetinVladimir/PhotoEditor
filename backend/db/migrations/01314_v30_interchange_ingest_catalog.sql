-- +goose Up
-- Imported originals are already charged/adopted parse artifacts: catalog
-- registration adds references, not duplicate object bytes or upload settlement.
-- +goose StatementBegin
CREATE TABLE photo.v30_interchange_ingest_outcomes(
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,ordinal integer NOT NULL,
 state text NOT NULL CHECK(state IN('adopted','rejected')),
 master_id uuid,master_sha text,failure_stage text,failure_reason text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,run_id,ordinal),
 FOREIGN KEY(tenant_id,run_id,ordinal) REFERENCES photo.v30_interchange_ingest_bindings(tenant_id,run_id,ordinal),
 FOREIGN KEY(tenant_id,master_id) REFERENCES photo.artifacts(tenant_id,id),
 CHECK((state='adopted' AND master_id IS NOT NULL AND master_sha IS NOT NULL AND master_sha ~ '^[0-9a-f]{64}$' AND failure_stage IS NULL AND failure_reason IS NULL)
 OR(state='rejected' AND master_id IS NULL AND master_sha IS NULL AND failure_stage IS NOT NULL AND failure_reason IS NOT NULL AND failure_stage IN('asset.verify','asset.inspect','asset.decode') AND failure_reason IN('failed','cancelled')))
);
ALTER TABLE photo.v30_interchange_ingest_outcomes ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_interchange_ingest_outcomes FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_interchange_ingest_outcomes TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v30_interchange_ingest_outcomes FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT SELECT,INSERT ON photo.v30_interchange_ingest_outcomes TO photo_fn_owner;
CREATE TRIGGER v30_interchange_ingest_outcomes_immutable BEFORE UPDATE ON photo.v30_interchange_ingest_outcomes FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();

CREATE FUNCTION photo.adopt_v30_interchange_ingest_image(p_tenant uuid,p_actor uuid,p_session uuid,p_run uuid,p_ordinal integer,p_master uuid,p_sha text,p_image text,p_metadata text,p_metadata_id uuid,p_metadata_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.v30_interchange_ingest_bindings%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 old photo.v30_interchange_ingest_outcomes%ROWTYPE;image jsonb;metadata jsonb;leaf jsonb;role_name text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576
 OR p_metadata IS NULL OR octet_length(p_metadata) NOT BETWEEN 2 AND 16384 OR encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO b FROM photo.v30_interchange_ingest_bindings WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal;
 IF r.id IS NULL OR b.run_id IS NULL OR r.kind IS DISTINCT FROM 'ingest' OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor
 OR b.actor_id IS DISTINCT FROM p_actor OR photo.v2_editor_authorized(p_tenant,b.project_id,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=b.project_id AND archived_at IS NULL) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.v30_interchange_ingest_outcomes WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal;
 IF FOUND THEN RETURN old.state='adopted' AND old.master_id=p_master AND old.master_sha=p_sha
 AND EXISTS(SELECT 1 FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=b.version_id AND asset_id=b.asset_id AND master_artifact_id=p_master AND content_hash=p_sha);END IF;
 IF r.status NOT IN('queued','running') OR NOT r.graph_sealed OR NOT EXISTS(SELECT 1 FROM photo.ingest_progress WHERE tenant_id=p_tenant AND run_id=p_run AND phase IN('decode','catalog')) THEN RETURN false;END IF;
 image:=p_image::jsonb;metadata:=p_metadata::jsonb;
 IF metadata->'source' IS DISTINCT FROM jsonb_build_object('artifact_id',b.source_id,'sha256',b.source_sha)
 OR image->'canvas'->>'source_sha256' IS DISTINCT FROM b.source_sha OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1' THEN RETURN false;END IF;
 -- Inspect bytes must match the producer's exact ready metadata output too.
 PERFORM a.id FROM photo.ingest_node_plans n JOIN photo.tasks it ON it.tenant_id=n.tenant_id AND it.id=n.id
 JOIN photo.task_results iz ON iz.tenant_id=it.tenant_id AND iz.id=it.canonical_result_id AND iz.task_id=it.id
 JOIN photo.artifacts a ON a.tenant_id=it.tenant_id AND a.created_by_task_id=it.id
 WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.ordinal=p_ordinal AND n.operation='asset.inspect'
 AND n.source_artifact_id=b.source_id AND n.source_sha=b.source_sha AND it.state='succeeded'
 AND a.output_role='metadata' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.id=p_metadata_id AND a.sha256=p_metadata_sha AND a.bytes BETWEEN 2 AND 1048576
 AND iz.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) FOR SHARE OF a,it;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT task.* INTO t FROM photo.ingest_node_plans n JOIN photo.tasks task ON task.tenant_id=n.tenant_id AND task.id=n.id
 WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.ordinal=p_ordinal AND n.operation='asset.decode'
 AND n.source_artifact_id=b.source_id AND n.source_sha=b.source_sha AND task.state='succeeded' FOR SHARE OF task;
 IF t.id IS NULL OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=t.canonical_result_id;
 IF z.id IS NULL THEN RETURN false;END IF;
 FOR role_name,leaf IN SELECT 'working_image',jsonb_build_object('artifact_id',p_master,'sha256',p_sha)
 UNION ALL SELECT 'working_rgb',image->'pixels'
 UNION ALL SELECT 'alpha',image->'alpha' WHERE image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb LOOP
  IF jsonb_typeof(leaf) IS DISTINCT FROM 'object' OR NOT(z.outputs @> jsonb_build_array(leaf)) THEN RETURN false;END IF;
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(leaf->>'artifact_id')::uuid AND sha256=leaf->>'sha256'
  AND state='ready' AND verified_at IS NOT NULL AND object_version NOT IN('','null') AND created_by_task_id=t.id AND output_role=role_name
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=b.source_id AND sha256=b.source_sha AND state='ready'
 AND verified_at IS NOT NULL AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 INSERT INTO photo.assets(id,tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance)
 VALUES(b.asset_id,p_tenant,b.project_id,b.source_id,image||jsonb_build_object('ingest',metadata),'verified',
 jsonb_build_object('kind','interchange','workflow_id',b.workflow_id,'parsed_id',b.parsed_id,'parsed_sha',b.parsed_sha,'entry_id',b.entry_id,'run_id',p_run,'ordinal',p_ordinal,'source_sha256',b.source_sha));
 INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,created_by_run_id,content_hash,metadata,review_state,version_no)
 VALUES(b.version_id,p_tenant,b.asset_id,p_master,p_run,p_sha,image||jsonb_build_object('ingest',metadata),'pending_review',1);
 INSERT INTO photo.v30_interchange_ingest_outcomes(tenant_id,run_id,ordinal,state,master_id,master_sha)
 VALUES(p_tenant,p_run,p_ordinal,'adopted',p_master,p_sha);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_v30_interchange_ingest_image(uuid,uuid,uuid,uuid,integer,uuid,text,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_interchange_ingest_image(uuid,uuid,uuid,uuid,integer,uuid,text,text,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_interchange_ingest_image(uuid,uuid,uuid,uuid,integer,uuid,text,text,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
