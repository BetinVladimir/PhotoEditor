-- +goose Up
-- Keep the frozen admission evidence locked through atomic assembly publication.
-- Existing UPDATE(id) grants from 00435 permit FOR SHARE; no new grants needed.
-- +goose StatementBegin
ALTER FUNCTION photo.commit_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb)
 RENAME TO commit_v27_semantic_assembly_pre_qualification;
REVOKE ALL ON FUNCTION photo.commit_v27_semantic_assembly_pre_qualification(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.commit_v27_semantic_assembly(p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid,
 p_entries jsonb,p_manifest jsonb,p_chunks jsonb,p_entries_sha text,p_direct jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; accepted boolean;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT config_snapshot->'v27_semantic'->'qualification' INTO pin FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 PERFORM id FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_expansion FOR UPDATE;
 IF NOT photo.v27_semantic_frozen_qualification_ready(p_tenant,p_run) THEN RETURN false;END IF;
 PERFORM id FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid FOR SHARE;
 PERFORM id FROM photo.model_qualifications WHERE tenant_id=p_tenant AND id=(pin->>'qualification_id')::uuid FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN((pin->'report'->>'artifact_id')::uuid,(pin->'calibration'->>'artifact_id')::uuid) ORDER BY id FOR SHARE;
 IF NOT photo.v27_semantic_frozen_qualification_ready(p_tenant,p_run)
 OR jsonb_typeof(p_entries) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_entries) e WHERE e->>'contract'='Calibration' AND e->'ref'=pin->'calibration') THEN RETURN false;END IF;
 accepted:=photo.commit_v27_semantic_assembly_pre_qualification(p_tenant,p_run,p_expansion,p_token,p_task,p_entries,p_manifest,p_chunks,p_entries_sha,p_direct);
 IF accepted AND NOT photo.v27_semantic_frozen_qualification_ready(p_tenant,p_run) THEN
 RAISE EXCEPTION 'SEMANTIC_COMMIT_QUALIFICATION_STALE' USING ERRCODE='23514';END IF;
 RETURN accepted;
END $$;
ALTER FUNCTION photo.commit_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.commit_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
