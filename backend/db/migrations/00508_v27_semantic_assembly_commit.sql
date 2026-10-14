-- +goose Up
-- Generic registration requires an unsealed graph. Unseal/register/seal/close
-- happen within this one statement; an error rolls back every intermediate row.
-- +goose StatementBegin
CREATE FUNCTION photo.commit_v27_semantic_assembly(p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid,
 p_entries jsonb,p_manifest jsonb,p_chunks jsonb,p_entries_sha text,p_direct jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; x record; n integer; pos integer:=0; batch jsonb; registered integer;
BEGIN
 IF jsonb_typeof(p_entries) IS DISTINCT FROM 'array' OR jsonb_typeof(p_direct) IS DISTINCT FROM 'array'
 OR p_entries_sha IS NULL OR p_entries_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 n:=jsonb_array_length(p_entries);
 IF n NOT BETWEEN 4 AND 50000 OR pg_column_size(p_entries)>16777216 OR jsonb_array_length(p_direct)<>4 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 PERFORM id FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_expansion FOR UPDATE;
 -- Retain authority/catalog/producers until atomic publication completes.
 PERFORM id FROM photo.users WHERE id=r.created_by FOR SHARE;
 PERFORM id FROM photo.sessions WHERE user_id=r.created_by AND id=(r.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid FOR SHARE;
 PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=r.created_by FOR SHARE;
 PERFORM p.id FROM photo.projects p JOIN photo.v2_workflows w ON w.tenant_id=p.tenant_id AND w.project_id=p.id
 WHERE w.tenant_id=p_tenant AND w.run_id=p_run FOR SHARE OF p;
 PERFORM a.user_id FROM photo.project_acl a JOIN photo.v2_workflows w ON w.tenant_id=a.tenant_id AND w.project_id=a.project_id
 WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND a.user_id=r.created_by FOR SHARE OF a;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id FOR SHARE;
 PERFORM v.id FROM photo.asset_versions v JOIN photo.snapshot_items i ON i.tenant_id=v.tenant_id AND i.asset_version_id=v.id
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id ORDER BY v.id FOR SHARE OF v;
 PERFORM a.id FROM photo.assets a JOIN photo.asset_versions v ON v.tenant_id=a.tenant_id AND v.asset_id=a.id
 JOIN photo.snapshot_items i ON i.tenant_id=v.tenant_id AND i.asset_version_id=v.id
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id ORDER BY a.id FOR SHARE OF a;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run ORDER BY id FOR UPDATE;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND
 (EXISTS(SELECT 1 FROM jsonb_array_elements(p_entries) e WHERE e->'ref'->>'artifact_id'=a.id::text)
 OR EXISTS(SELECT 1 FROM photo.assets asset JOIN photo.asset_versions v ON v.tenant_id=asset.tenant_id AND v.asset_id=asset.id
 JOIN photo.snapshot_items i ON i.tenant_id=v.tenant_id AND i.asset_version_id=v.id
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id AND asset.original_artifact_id=a.id)
 OR EXISTS(SELECT 1 FROM photo.tasks producer WHERE producer.tenant_id=p_tenant AND producer.run_id=p_run AND producer.params_artifact_id=a.id))
 ORDER BY a.id FOR SHARE OF a;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF NOT photo.v27_semantic_assembly_lease_live(p_tenant,p_run,p_expansion,p_token)
 OR NOT photo.v27_semantic_transport_scope(p_tenant,p_run,p_task,p_token)
 OR t.execution_profile ? 'v2_input_manifest'
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task) THEN RETURN false;END IF;
 FOR x IN SELECT value AS item FROM jsonb_array_elements(p_entries) LOOP
 IF x.item->>'contract' NOT IN('Params','EditIntent','Calibration','InputSnapshot','TransferCandidate','WorkingImage','MaskRef','ResolvedTransform','NpyArray') THEN RETURN false;END IF;
 END LOOP;
 FOR x IN SELECT unnest(ARRAY['Params','EditIntent','Calibration','InputSnapshot']) AS contract LOOP
 IF (SELECT count(*) FROM jsonb_array_elements(p_entries) a WHERE a->>'contract'=x.contract)<>1
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_entries) a WHERE a->>'contract'=x.contract AND p_direct @> jsonb_build_array(a->'ref')) THEN RETURN false;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_entries) a WHERE a->>'contract'='Params'
 AND a->'ref'->>'artifact_id'=t.params_artifact_id::text)
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_entries) a WHERE a->>'contract'='InputSnapshot'
 AND a->'ref'=r.config_snapshot->'v27_semantic'->'snapshot') THEN RETURN false;END IF;
 SELECT count(*) INTO registered FROM photo.v27_semantic_assembly_inputs(p_tenant,p_run);
 IF registered NOT BETWEEN 1 AND 5000 OR registered<>(SELECT count(*) FROM jsonb_array_elements(p_entries) a WHERE a->>'contract'='TransferCandidate') THEN RETURN false;END IF;
 FOR x IN SELECT * FROM photo.v27_semantic_assembly_inputs(p_tenant,p_run) LOOP
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_entries) a WHERE a->>'contract'='TransferCandidate'
 AND a->'ref'=jsonb_build_object('artifact_id',x.artifact_id,'sha256',x.sha256))
 OR NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run AND prerequisite_id=x.task_id AND dependent_id=p_task AND condition='success') THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT count(*) FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run AND dependent_id=p_task)<>registered THEN RETURN false;END IF;
 FOR x IN SELECT artifact_id,sha256 FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=r.input_snapshot_id LOOP
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_entries) a WHERE a->>'contract'='WorkingImage'
 AND a->'ref'=jsonb_build_object('artifact_id',x.artifact_id,'sha256',x.sha256)) THEN RETURN false;END IF;
 END LOOP;
 -- Locks retained until statement completion prevent any observable half graph.
 UPDATE photo.runs SET graph_sealed=false WHERE tenant_id=p_tenant AND id=p_run;
 WHILE pos<n LOOP
 SELECT jsonb_agg(a.value ORDER BY a.ordinality) INTO batch FROM jsonb_array_elements(p_entries) WITH ORDINALITY a
 WHERE a.ordinality>pos AND a.ordinality<=pos+512;
 registered:=photo.register_v2_task_input_batch(p_tenant,p_run,p_task,pos,batch);
 IF registered<>jsonb_array_length(batch) THEN RAISE EXCEPTION 'SEMANTIC_INPUT_BATCH_INCOMPLETE' USING ERRCODE='23514';END IF;
 pos:=pos+registered;
 END LOOP;
 IF NOT photo.seal_v2_task_inputs(p_tenant,p_run,p_task,p_manifest,p_chunks,n,p_entries_sha,p_direct) THEN RAISE EXCEPTION 'SEMANTIC_INPUT_SEAL_FAILED' USING ERRCODE='23514';END IF;
 IF NOT photo.v2_input_closure_ready(p_task) THEN RAISE EXCEPTION 'SEMANTIC_INPUT_CLOSURE_INCOMPLETE' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET graph_sealed=true WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT photo.v27_semantic_assembly_lease_live(p_tenant,p_run,p_expansion,p_token)
 OR NOT EXISTS(SELECT 1 FROM photo.v27_semantic_assembly_inputs(p_tenant,p_run)) THEN
 RAISE EXCEPTION 'SEMANTIC_COMMIT_AUTHORITY_STALE' USING ERRCODE='23514';END IF;
 UPDATE photo.graph_expansions SET state='closed',expansion_token=NULL,expansion_lease_until=NULL
 WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_expansion AND expansion_token=p_token;
 RETURN true;
END $$;
ALTER FUNCTION photo.commit_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.commit_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
