-- +goose Up
-- Freeze versioned source locations for one live, complete assembly claim.
-- The compiler consumes bytes after this DB call; commit repeats live authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_assembly_source_inventory(p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid)
RETURNS TABLE(task_id uuid,artifact_id uuid,sha256 text,contract text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;
BEGIN
 IF NOT photo.v27_semantic_assembly_lease_live(p_tenant,p_run,p_expansion,p_token) THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 RETURN QUERY WITH ready AS MATERIALIZED(SELECT * FROM photo.v27_semantic_assembly_inputs(p_tenant,p_run)),
 declared AS (
 SELECT t.id AS owner,i.artifact_id AS aid,i.sha256 AS digest,i.contract AS kind
 FROM ready x JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=x.task_id AND t.run_id=p_run
 JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND i.task_id=t.id
 WHERE i.contract IN('Params','EditIntent','Calibration')
 UNION ALL SELECT x.task_id,x.artifact_id,x.sha256,'TransferCandidate' FROM ready x
 UNION ALL SELECT NULL::uuid,i.artifact_id,i.sha256,'WorkingImage' FROM photo.snapshot_items i
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id AND EXISTS(SELECT 1 FROM ready)
 UNION ALL SELECT NULL::uuid,(r.config_snapshot->'v27_semantic'->'snapshot'->>'artifact_id')::uuid,
 r.config_snapshot->'v27_semantic'->'snapshot'->>'sha256','InputSnapshot' WHERE EXISTS(SELECT 1 FROM ready))
 SELECT DISTINCT d.owner,a.id,a.sha256,d.kind,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM declared d JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=d.aid AND a.sha256=d.digest
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND CASE WHEN d.kind='InputSnapshot' THEN 8388608 ELSE 1048576 END
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 ORDER BY d.owner,a.id,d.kind;
EXCEPTION WHEN invalid_text_representation THEN RETURN;
END $$;
ALTER FUNCTION photo.v27_semantic_assembly_source_inventory(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_assembly_source_inventory(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_assembly_source_inventory(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
