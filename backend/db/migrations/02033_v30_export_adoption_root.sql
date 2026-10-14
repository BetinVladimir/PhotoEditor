-- +goose Up
-- The API derives the root from the current canonical result, then repeats
-- the full export output gate. Neither a caller nor the pending ledger chooses
-- an output artifact for result adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_root(
 p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;wid uuid;tid uuid;root_id uuid;root_sha text;z photo.task_results%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 120
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);
 wid:=photo.v30_uuid5(rid,'workflow');tid:=photo.v30_uuid5(wid,'v2.interchange.export:0');
 SELECT result.* INTO z FROM photo.tasks task JOIN photo.task_results result
  ON result.tenant_id=task.tenant_id AND result.id=task.canonical_result_id AND result.task_id=task.id
 WHERE task.tenant_id=p_tenant AND task.run_id=rid AND task.id=tid
  AND task.operation='v2.interchange.export' AND task.state='succeeded'
  AND NOT task.cancel_requested AND result.attempt=task.attempt
  AND result.fencing_token=task.fencing_token FOR SHARE OF task,result;
 IF z.id IS NULL OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array'
 OR jsonb_array_length(z.outputs) NOT BETWEEN 3 AND 53 THEN RETURN;END IF;
 root_id:=(z.outputs->0->>'artifact_id')::uuid;
 root_sha:=z.outputs->0->>'sha256';
 IF root_id IS NULL OR root_sha IS NULL THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_interchange_export_output(p_tenant,p_actor,p_session,p_key,p_request_sha,
  root_id,root_sha,'InterchangeResult');
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT root_id,root_sha;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_root(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_root(uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_root(uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
