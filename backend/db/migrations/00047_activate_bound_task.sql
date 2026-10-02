-- +goose Up
-- Compiler supplies the immutable digest after resolving actual input bindings.
-- This is a single-node CAS, not graph sealing or task resource admission.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_bound_task(p_task uuid,p_digest text,p_params uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;edge record;ref jsonb;
BEGIN
 IF p_task IS NULL OR p_params IS NULL OR p_digest IS NULL OR p_digest !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 t:=photo.lock_task(p_task);
 IF t.id IS NULL OR t.state NOT IN ('blocked','ready') OR t.input_digest<>p_digest OR t.params_artifact_id<>p_params
 OR t.cancel_requested OR t.terminal_reason IS NOT NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF r.cancel_requested OR NOT r.graph_sealed OR r.status NOT IN ('queued','running','waiting_review')
 OR EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=t.tenant_id AND e.run_id=t.run_id AND e.state<>'closed') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=t.tenant_id AND a.id=p_params AND a.state='ready'
 AND a.media_type='application/json' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF jsonb_typeof(t.inputs) IS DISTINCT FROM 'array' OR jsonb_array_length(t.inputs)>128 THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(t.inputs) LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}'
  OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=t.tenant_id AND a.id::text=ref->>'artifact_id'
   AND a.sha256=ref->>'sha256' AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 FOR edge IN SELECT e.condition,p.id,p.state,p.terminal_reason,p.canonical_result_id FROM photo.task_edges e
 JOIN photo.tasks p ON p.tenant_id=e.tenant_id AND p.run_id=e.run_id AND p.id=e.prerequisite_id
 WHERE e.tenant_id=t.tenant_id AND e.run_id=t.run_id AND e.dependent_id=t.id LOOP
  IF edge.state NOT IN ('succeeded','failed','cancelled','skipped') THEN RETURN false;END IF;
  IF edge.condition='terminal' THEN CONTINUE;END IF;
  IF (edge.state='succeeded' AND edge.canonical_result_id IS NOT NULL OR edge.state='skipped' AND edge.terminal_reason='disabled') IS NOT TRUE THEN RETURN false;END IF;
  IF edge.condition='success' THEN CONTINUE;END IF;
  IF edge.condition NOT IN ('accepted','rejected') THEN RETURN false;END IF;
  IF NOT EXISTS(SELECT FROM photo.reviews v WHERE v.tenant_id=t.tenant_id AND v.run_id=t.run_id AND v.task_id=edge.id)
  OR EXISTS(SELECT FROM photo.reviews v WHERE v.tenant_id=t.tenant_id AND v.run_id=t.run_id AND v.task_id=edge.id
   AND (v.state<>'decided' OR v.decision IS DISTINCT FROM CASE edge.condition WHEN 'accepted' THEN 'accept' ELSE 'reject' END)) THEN RETURN false;END IF;
 END LOOP;
 IF t.state='blocked' THEN UPDATE photo.tasks SET state='ready' WHERE id=t.id AND tenant_id=t.tenant_id;END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.activate_bound_task(uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_bound_task(uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.activate_bound_task(uuid,text,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
