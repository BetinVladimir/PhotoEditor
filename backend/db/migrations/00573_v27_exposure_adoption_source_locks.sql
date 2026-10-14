-- +goose Up
-- Retain source/version/snapshot/declared-input predicates through commit.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.adopt_semantic_exposure(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text,
 p_params uuid,p_sha text,p_body text,p_root uuid,p_root_sha text,p_binary uuid,p_binary_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;e jsonb;project uuid;task_ids uuid[];accepted boolean;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 e:=p_body::jsonb;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL OR t.operation<>'semantic.local_exposure' OR t.operation_version<>'1' THEN RETURN false;END IF;
 SELECT (r.config_snapshot->>'project_id')::uuid INTO project FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=t.run_id;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT array_agg(DISTINCT value) INTO task_ids FROM (
 SELECT p_task AS value UNION ALL SELECT created_by_task_id FROM photo.artifacts WHERE tenant_id=p_tenant
 AND id IN((e->'candidate'->>'artifact_id')::uuid,p_root,p_binary)) ids WHERE value IS NOT NULL;
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id IN(SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(task_ids)) ORDER BY r.id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(task_ids) ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=p_tenant AND id=(e->>'target_id')::uuid FOR UPDATE;
 PERFORM v.id FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND v.id=(e->'source'->>'asset_version_id')::uuid FOR SHARE;
 PERFORM asset.id FROM photo.assets asset JOIN photo.asset_versions v ON v.tenant_id=asset.tenant_id AND v.asset_id=asset.id
 WHERE v.tenant_id=p_tenant AND v.id=(e->'source'->>'asset_version_id')::uuid FOR SHARE OF asset;
 PERFORM si.ordinal FROM photo.snapshot_items si JOIN photo.runs r ON r.tenant_id=si.tenant_id AND r.input_snapshot_id=si.snapshot_id
 WHERE r.tenant_id=p_tenant AND r.id=t.run_id AND si.asset_version_id=(e->'source'->>'asset_version_id')::uuid FOR SHARE OF si;
 -- v2_task_inputs are immutable; their purge is serialized by the tenant lock.
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(
 p_params,p_root,p_binary,(e->'candidate'->>'artifact_id')::uuid,(e->'intent'->>'artifact_id')::uuid,
 (e->'source'->'source'->>'artifact_id')::uuid,(e->'mask'->>'artifact_id')::uuid,(e->'transform'->>'artifact_id')::uuid)
 ORDER BY a.id FOR SHARE;
 IF NOT photo.v27_exposure_adoption_protection_origin(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_sha,p_body,p_root,p_root_sha,p_binary,p_binary_sha) THEN RETURN false;END IF;
 accepted:=photo.adopt_semantic_exposure_inventory(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
 IF accepted AND NOT photo.v27_exposure_adoption_protection_origin(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_sha,p_body,p_root,p_root_sha,p_binary,p_binary_sha) THEN
 RAISE EXCEPTION 'SEMANTIC_EXPOSURE_ADOPTION_STALE' USING ERRCODE='23514';END IF;
 RETURN accepted;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_semantic_exposure(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_semantic_exposure(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_semantic_exposure(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,uuid,text) TO photo_scheduler;


-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
