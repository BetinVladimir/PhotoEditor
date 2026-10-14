-- +goose Up
-- A replay acknowledges an already durable blocked transport seal only.
-- It is not fresh source authorization and cannot activate the graph.
ALTER TABLE photo.runs ADD COLUMN v29_perspective_lines_seal_receipt jsonb;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_lines_seal_receipt_scope CHECK (
 v29_perspective_lines_seal_receipt IS NULL OR
 (v29_perspective_line_detection AND jsonb_typeof(v29_perspective_lines_seal_receipt)='object'
 AND pg_column_size(v29_perspective_lines_seal_receipt)<=524288));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_lines_seal_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_perspective_lines_seal_receipt IS NOT NULL THEN
   RAISE EXCEPTION 'Seal receipt requires completed transport seal' USING ERRCODE='42501';END IF;
 ELSIF NEW.v29_perspective_lines_seal_receipt IS DISTINCT FROM OLD.v29_perspective_lines_seal_receipt THEN
  IF current_user<>'photo_fn_owner' OR OLD.v29_perspective_lines_seal_receipt IS NOT NULL
   OR NOT OLD.v29_perspective_line_detection OR OLD.status<>'draft' OR OLD.graph_sealed OR OLD.cancel_requested
   OR NEW.status IS DISTINCT FROM OLD.status OR NEW.graph_sealed IS DISTINCT FROM OLD.graph_sealed
   OR NEW.cancel_requested IS DISTINCT FROM OLD.cancel_requested THEN
   RAISE EXCEPTION 'Seal receipt is immutable' USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_lines_seal_receipt() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_lines_seal_receipt() FROM PUBLIC;
CREATE TRIGGER runs_v29_lines_seal_receipt_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_lines_seal_receipt();

ALTER FUNCTION photo.seal_v29_perspective_lines_transport(uuid,uuid,jsonb,jsonb) RENAME TO seal_v29_perspective_lines_transport_first;
REVOKE ALL ON FUNCTION photo.seal_v29_perspective_lines_transport_first(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v29_perspective_lines_transport(p_tenant uuid,p_run uuid,p_proof jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;receipt jsonb;plan jsonb;params jsonb;expected_profile jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_proof IS NULL OR p_witness IS NULL OR pg_column_size(p_proof)>262144 OR pg_column_size(p_witness)>131072
 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 receipt:=jsonb_build_object('schema_version',1,'proof',p_proof,'witness',p_witness);
 IF r.v29_perspective_lines_seal_receipt IS NOT NULL THEN
  IF r.v29_perspective_lines_seal_receipt IS DISTINCT FROM receipt THEN RETURN false;END IF;
  plan:=r.config_snapshot->'v29_perspective_line_detection';
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(plan->>'task_id')::uuid FOR UPDATE;
  IF NOT FOUND OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested OR t.canonical_result_id IS NOT NULL THEN RETURN false;END IF;
  SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO params
  FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=t.id
  AND purpose='task_params' AND state='ready' AND artifact_id=t.params_artifact_id;
  IF params IS NULL OR r.v29_perspective_lines_execution_policy IS NULL THEN RETURN false;END IF;
  expected_profile:=(plan->>'profile_jcs')::jsonb || jsonb_build_object(
  'contracts_sha','341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815',
  'v2_input_manifest',p_proof->'manifest','v2_entry_count',p_proof->'entry_count','v2_entries_sha',p_proof->>'entries_sha',
  'runtime_lock_sha',(r.v29_perspective_lines_execution_policy::jsonb)->>'runtime_lock_sha',
  'execution_policy_sha',encode(sha256(convert_to(r.v29_perspective_lines_execution_policy,'UTF8')),'hex'));
  RETURN t.inputs=jsonb_build_array(p_proof->'manifest',params) AND t.execution_profile=expected_profile
   AND photo.v2_input_closure_ready(t.id);
 END IF;
 IF photo.seal_v29_perspective_lines_transport_first(p_tenant,p_run,p_proof,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.runs SET v29_perspective_lines_seal_receipt=receipt WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.seal_v29_perspective_lines_transport(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v29_perspective_lines_transport(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.seal_v29_perspective_lines_transport(uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
