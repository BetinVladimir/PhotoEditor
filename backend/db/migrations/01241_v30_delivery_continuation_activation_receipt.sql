-- +goose Up
-- Acknowledge activation even if the child later failed or reopened its graph.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_continuation_activated(p_expansion uuid,p_token uuid,p_slot integer,p_stage integer,
 p_predecessor uuid,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;cfg jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 b photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;previous photo.tasks%ROWTYPE;d photo.backend_documents%ROWTYPE;
 operation text;previous_operation text;required_stage integer;owner uuid;
BEGIN
 IF p_stage IS NULL OR p_stage NOT BETWEEN 1 AND 2 OR p_predecessor IS NULL OR p_task IS NULL
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF request IS NULL THEN RETURN false;END IF;
 cfg:=(request->>'config_jcs')::jsonb;operation:=p_task->>'operation';
 IF operation='derivative.sharpen' THEN
  IF cfg->'size' IS NULL OR cfg->'sharpen' IS NULL THEN RETURN false;END IF;
  required_stage:=1;previous_operation:='derivative.resize';
 ELSIF operation='export.encode' THEN
  IF cfg->'size' IS NULL AND cfg->'sharpen' IS NULL THEN RETURN false;END IF;
  required_stage:=CASE WHEN cfg->'size' IS NOT NULL AND cfg->'sharpen' IS NOT NULL THEN 2 ELSE 1 END;
  previous_operation:=CASE WHEN cfg->'sharpen' IS NOT NULL THEN 'derivative.sharpen' ELSE 'derivative.resize' END;
 ELSE RETURN false;END IF;
 IF p_stage<>required_stage THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(request->>'child_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id AND kind='v30.delivery.export/1' FOR SHARE;
 IF NOT FOUND OR b.cursor !~ '^[0-2]$' OR b.cursor::integer<p_stage THEN RETURN false;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),operation||':0');
 IF p_predecessor<>photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),previous_operation||':0') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=owner FOR SHARE;
 IF NOT FOUND OR t.state NOT IN('ready','leased','running','succeeded','failed','cancelled','skipped')
 OR t.id::text IS DISTINCT FROM p_task->>'id' OR t.node_key IS DISTINCT FROM operation||':0'
 OR t.node_key IS DISTINCT FROM p_task->>'node_key' OR t.operation IS DISTINCT FROM operation
 OR t.operation_version IS DISTINCT FROM p_task->>'operation_version' OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
 OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR (t.execution_profile-'v2_input_manifest') IS DISTINCT FROM p_task->'execution_profile'
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=t.id
 AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready';
 IF NOT FOUND OR d.sha256 IS DISTINCT FROM p_task->>'params_sha'
 OR d.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_params,'UTF8')),'hex') OR d.bytes<>octet_length(p_params) THEN RETURN false;END IF;
 SELECT * INTO previous FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=p_predecessor;
 IF NOT FOUND OR previous.canonical_result_id IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(previous.id,previous.canonical_result_id))
 OR NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=e.tenant_id AND run_id=r.id
 AND prerequisite_id=p_predecessor AND dependent_id=t.id AND condition='success') THEN RETURN false;END IF;
 IF operation='export.encode' AND NOT EXISTS(SELECT 1 FROM photo.exports WHERE tenant_id=e.tenant_id
 AND id=(r.config_snapshot->>'export_id')::uuid AND task_id=t.id) THEN RETURN false;END IF;
 RETURN e.expansion_lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_continuation_activated(uuid,uuid,integer,integer,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_continuation_activated(uuid,uuid,integer,integer,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_continuation_activated(uuid,uuid,integer,integer,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
