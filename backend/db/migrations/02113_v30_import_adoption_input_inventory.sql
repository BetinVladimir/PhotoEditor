-- +goose Up
-- Scheduler-only typed inventory for independent prepared-recipe closure.
-- A reader must still verify each exact object version and repeat this lookup
-- after physical IO; this function never authorizes result publication.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_adoption_input_inventory(p_tenant uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(ordinal integer,artifact_id uuid,sha256 text,contract text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE declared_count integer;physical_count integer;
BEGIN
 IF photo.v30_interchange_import_adoption_context(p_tenant,p_task,p_attempt,
  p_fence,p_result) IS NULL THEN RETURN;END IF;
 SELECT count(*) INTO declared_count FROM photo.v2_task_inputs i
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task;
 IF declared_count NOT BETWEEN 2 AND 30000 THEN RETURN;END IF;
 SELECT count(*) INTO physical_count FROM photo.v2_task_inputs i
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
  AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND a.state='ready'
  AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
  AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 2 AND 137438953472
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF physical_count<>declared_count THEN RETURN;END IF;
 RETURN QUERY SELECT i.ordinal,i.artifact_id,i.sha256,i.contract,a.media_type
 FROM photo.v2_task_inputs i JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task ORDER BY i.ordinal;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_adoption_input_inventory(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_adoption_input_inventory(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_adoption_input_inventory(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
