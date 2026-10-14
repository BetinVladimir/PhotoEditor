-- +goose Up
-- Exact native graph and live reservation, retained after all evidence locks.
-- Does not activate, seal or publish a queued event.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_graph_budget(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;w photo.v2_workflows%ROWTYPE;budget numeric;matched integer:=0;candidate record;
BEGIN
 IF jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432
 OR p_evidence->>'operation' IS NULL OR p_evidence->>'operation' NOT IN ('face.detect','mask.person') THEN RETURN false;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.failure_policy<>'fail_fast' OR r.created_by::text IS DISTINCT FROM p_evidence->>'actor_id' THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2' OR w.input_snapshot_id::text IS DISTINCT FROM (p_evidence->'anchor'->>'intent_text')::jsonb->>'target_snapshot_id' THEN RETURN false;END IF;
 FOR candidate IN SELECT id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run ORDER BY id FOR UPDATE LOOP
  matched:=matched+1;
  IF candidate.id::text IS DISTINCT FROM p_evidence->'registration'->>'task_id' THEN RETURN false;END IF;
 END LOOP;
 IF matched<>1 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run) THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_evidence->'registration'->>'task_id')::uuid;
 IF NOT FOUND OR t.state<>'blocked' OR t.cancel_requested OR t.attempt<>0 OR t.operation IS DISTINCT FROM p_evidence->>'operation' OR t.operation_version<>'1'
 OR t.resource_class<>'gpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cuda' OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN false;END IF;
 -- Native outputs retain their established roles. Generic V2 document
 -- output policy / v2_enabled activation are separate gateway/seal obligations.
 IF NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation AND op.version=t.operation_version AND op.resource_class=t.resource_class AND op.backends ? (t.execution_profile->>'backend')) THEN RETURN false;END IF;
 IF t.operation='face.detect' THEN
  IF (SELECT count(*) FROM photo.operation_output_policies WHERE operation=t.operation AND version=t.operation_version)<>1
  OR NOT EXISTS(SELECT 1 FROM photo.operation_output_policies WHERE operation=t.operation AND version=t.operation_version AND role='face_boxes' AND media_types='["application/json"]'::jsonb AND max_count=1 AND max_bytes>0) THEN RETURN false;END IF;
 ELSE
  IF (SELECT count(*) FROM photo.operation_output_policies WHERE operation=t.operation AND version=t.operation_version)<>4
  OR EXISTS(SELECT 1 FROM (VALUES ('person_mask','application/octet-stream'),('person_mask_png','image/png'),('person_mask_ref','application/json'),('person_report','application/json')) expected(role,media)
   WHERE NOT EXISTS(SELECT 1 FROM photo.operation_output_policies policy WHERE policy.operation=t.operation AND policy.version=t.operation_version AND policy.role=expected.role AND policy.media_types=jsonb_build_array(expected.media) AND policy.max_count=1 AND policy.max_bytes>0)) THEN RETURN false;END IF;
 END IF;
 IF jsonb_typeof(t.execution_profile->'resource_estimate'->'output_bytes') IS DISTINCT FROM 'number' OR (t.execution_profile->'resource_estimate'->>'output_bytes') !~ '^[0-9]+$' THEN RETURN false;END IF;
 budget:=(t.execution_profile->'resource_estimate'->>'output_bytes')::numeric;
 IF budget NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 matched:=0;
 FOR q IN SELECT * FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run ORDER BY id FOR UPDATE LOOP
  matched:=matched+1;
  IF q.state<>'reserved' OR q.expires_at<=clock_timestamp() OR q.principal_type<>'user' OR q.principal_id IS DISTINCT FROM r.created_by OR q.files<>0
  OR q.bytes::numeric<budget+8388608 OR q.bytes>9007199254740991
  OR NOT EXISTS(SELECT 1 FROM photo.tenant_usage WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes) THEN RETURN false;END IF;
 END LOOP;
 RETURN matched=1 AND EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND status='draft' AND NOT cancel_requested AND NOT graph_sealed)
 AND EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=t.id AND state='blocked' AND NOT cancel_requested);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_graph_budget(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_graph_budget(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_graph_budget(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
