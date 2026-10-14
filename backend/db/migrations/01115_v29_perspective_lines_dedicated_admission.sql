-- +goose Up
-- A saved automatic-line run must never use generic closure/graph admission.
-- Dedicated admission/seal will retain the private prior implementation chain.
-- +goose StatementBegin
ALTER FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) RENAME TO register_v2_task_input_batch_pre_v29_lines;
REVOKE ALL ON FUNCTION photo.register_v2_task_input_batch_pre_v29_lines(uuid,uuid,uuid,integer,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.register_v2_task_input_batch(p_tenant uuid,p_run uuid,p_task uuid,p_first integer,p_rows jsonb)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND v29_perspective_line_detection) THEN
  RAISE EXCEPTION 'LINE_DETECTION_DEDICATED_ADMISSION_REQUIRED' USING ERRCODE='23514';END IF;
 RETURN photo.register_v2_task_input_batch_pre_v29_lines(p_tenant,p_run,p_task,p_first,p_rows);
END $$;
ALTER FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) TO photo_scheduler;

ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) RENAME TO seal_v2_task_inputs_pre_v29_lines;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs_pre_v29_lines(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_task_inputs(p_tenant uuid,p_run uuid,p_task uuid,p_manifest jsonb,p_chunks jsonb,p_entry_count integer,p_entries_sha text,p_direct_roots jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND v29_perspective_line_detection) THEN RETURN false;END IF;
 RETURN photo.seal_v2_task_inputs_pre_v29_lines(p_tenant,p_run,p_task,p_manifest,p_chunks,p_entry_count,p_entries_sha,p_direct_roots);
END $$;
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) TO photo_scheduler;

ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_v29_lines;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_v29_lines(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND v29_perspective_line_detection) THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v29_lines(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
