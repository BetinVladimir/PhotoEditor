-- +goose Up
-- Every registered donor input row must equal the stored compiler inventory.
-- Preserve previous behavior for unrelated V2 workflows.
-- +goose StatementBegin
ALTER FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) RENAME TO register_v2_task_input_batch_pre_v28_donor;
REVOKE ALL ON FUNCTION photo.register_v2_task_input_batch_pre_v28_donor(uuid,uuid,uuid,integer,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.register_v2_task_input_batch(p_tenant uuid,p_run uuid,p_task uuid,p_first integer,p_rows jsonb)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;expected jsonb;size integer;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V2_INPUT_TENANT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V2_INPUT_TENANT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.v28_donor_descriptor_jcs IS NOT NULL OR t.operation='portrait.open_eyes' THEN
  IF p_first IS NULL OR p_first NOT BETWEEN 0 AND 126 OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array'
   OR jsonb_array_length(p_rows) NOT BETWEEN 1 AND 127 OR p_first+jsonb_array_length(p_rows)>127
   OR photo.v28_donor_transport_scope(p_tenant,p_run,p_task,'v2_input_manifest') IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'V28_DONOR_INPUT_BATCH_UNAVAILABLE' USING ERRCODE='23514';END IF;
  size:=jsonb_array_length(p_rows);
  SELECT jsonb_agg(value ORDER BY ordinal) INTO expected
   FROM jsonb_array_elements((r.v28_donor_descriptor_jcs::jsonb)->'entries') WITH ORDINALITY entries(value,ordinal)
   WHERE ordinal>p_first AND ordinal<=p_first+size;
  IF expected IS DISTINCT FROM p_rows THEN RAISE EXCEPTION 'V28_DONOR_INPUT_BATCH_CHANGED' USING ERRCODE='23514';END IF;
 END IF;
 RETURN photo.register_v2_task_input_batch_pre_v28_donor(p_tenant,p_run,p_task,p_first,p_rows);
END $$;
ALTER FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_registered_inventory_ready(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT COALESCE(EXISTS(SELECT 1 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND t.id=p_task
   AND photo.v28_donor_transport_scope(p_tenant,p_run,p_task,'v2_input_manifest') IS TRUE
   AND (r.v28_donor_descriptor_jcs::jsonb)->'entries'=(SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.ordinal)
    FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract NOT IN('InputClosureChunk','InputClosureManifest'))),false);
$$;
ALTER FUNCTION photo.v28_donor_registered_inventory_ready(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_registered_inventory_ready(uuid,uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) RENAME TO seal_v2_task_inputs_pre_v28_donor;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs_pre_v28_donor(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_task_inputs(p_tenant uuid,p_run uuid,p_task uuid,p_manifest jsonb,p_chunks jsonb,
 p_entry_count integer,p_entries_sha text,p_direct_roots jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;entry jsonb;roots jsonb;body text:='[';separator text:='';
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.v28_donor_descriptor_jcs IS NOT NULL OR t.operation='portrait.open_eyes' THEN
  IF photo.v28_donor_registered_inventory_ready(p_tenant,p_run,p_task) IS DISTINCT FROM true
   OR p_entry_count IS DISTINCT FROM jsonb_array_length((r.v28_donor_descriptor_jcs::jsonb)->'entries')
   OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array' OR jsonb_array_length(p_chunks)<>1 THEN RETURN false;END IF;
  SELECT jsonb_agg(value->'ref' ORDER BY ordinal) INTO roots
   FROM jsonb_array_elements((r.v28_donor_descriptor_jcs::jsonb)->'entries') WITH ORDINALITY entries(value,ordinal);
  IF p_direct_roots IS DISTINCT FROM roots THEN RETURN false;END IF;
  FOR entry IN SELECT value FROM jsonb_array_elements((r.v28_donor_descriptor_jcs::jsonb)->'entries') LOOP
   body:=body||separator||'{"contract":"'||(entry->>'contract')||'","ref":{"artifact_id":"'||(entry->'ref'->>'artifact_id')||'","sha256":"'||(entry->'ref'->>'sha256')||'"}}';
   separator:=',';
  END LOOP;
  body:=body||']';
  IF p_entries_sha IS DISTINCT FROM encode(sha256(convert_to(body,'UTF8')),'hex') THEN RETURN false;END IF;
 END IF;
 RETURN photo.seal_v2_task_inputs_pre_v28_donor(p_tenant,p_run,p_task,p_manifest,p_chunks,p_entry_count,p_entries_sha,p_direct_roots);
END $$;
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
