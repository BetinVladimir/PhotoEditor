-- +goose Up
-- Persist initial blocked seal receipt; replay authorization remains separate.
-- It is not fresh source authorization and cannot activate the graph.
ALTER TABLE photo.runs ADD COLUMN v29_replacement_detector_seal_receipt jsonb;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_replacement_detector_seal_receipt_scope CHECK (
 v29_replacement_detector_seal_receipt IS NULL OR
 ((config_snapshot ? 'v29_perspective_replacement_detection_jcs') AND jsonb_typeof(v29_replacement_detector_seal_receipt)='object'
 AND pg_column_size(v29_replacement_detector_seal_receipt)<=4194304));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_replacement_detector_seal_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_replacement_detector_seal_receipt IS NOT NULL THEN
   RAISE EXCEPTION 'Seal receipt requires completed transport seal' USING ERRCODE='42501';END IF;
 ELSIF NEW.v29_replacement_detector_seal_receipt IS DISTINCT FROM OLD.v29_replacement_detector_seal_receipt THEN
  IF current_user<>'photo_fn_owner' OR OLD.v29_replacement_detector_seal_receipt IS NOT NULL
   OR NOT (OLD.config_snapshot ? 'v29_perspective_replacement_detection_jcs') OR OLD.status<>'draft' OR OLD.graph_sealed OR OLD.cancel_requested
   OR NEW.status IS DISTINCT FROM OLD.status OR NEW.graph_sealed IS DISTINCT FROM OLD.graph_sealed
   OR NEW.cancel_requested IS DISTINCT FROM OLD.cancel_requested THEN
   RAISE EXCEPTION 'Seal receipt is immutable' USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_replacement_detector_seal_receipt() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_replacement_detector_seal_receipt() FROM PUBLIC;
CREATE TRIGGER runs_v29_replacement_detector_seal_receipt_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_replacement_detector_seal_receipt();

ALTER FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) RENAME TO seal_v29_replacement_detector_transport_first;
REVOKE ALL ON FUNCTION photo.seal_v29_replacement_detector_transport_first(uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v29_replacement_detector_transport(p_tenant uuid,p_run uuid,p_proof jsonb,p_closure jsonb,p_witness jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_proof IS NULL OR p_closure IS NULL OR p_witness IS NULL OR pg_column_size(p_proof)>4194304
 OR pg_column_size(p_closure)>262144 OR pg_column_size(p_witness)>131072 THEN RETURN false;END IF;
 IF photo.seal_v29_replacement_detector_transport_first(p_tenant,p_run,p_proof,p_closure,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.runs SET v29_replacement_detector_seal_receipt=jsonb_build_object('schema_version',1,'descriptor_jcs',p_proof->>'descriptor_jcs','closure',p_closure,'witness',p_witness)
 WHERE tenant_id=p_tenant AND id=p_run AND v29_replacement_detector_seal_receipt IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'REPLACEMENT_DETECTOR_SEAL_RECEIPT_CONFLICT' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.seal_v29_replacement_detector_transport(uuid,uuid,jsonb,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
-- Historical acknowledgement only; caller must repeat fresh source authority
-- separately before activation. No repair or state mutation is performed.
CREATE FUNCTION photo.read_v29_replacement_detector_seal_receipt(p_tenant uuid,p_run uuid,p_descriptor text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;d jsonb;c jsonb;params jsonb;profile jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT FOUND OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR r.config_snapshot->>'v29_perspective_replacement_detection_jcs' IS DISTINCT FROM p_descriptor
 OR r.v29_replacement_detector_seal_receipt IS NULL
 OR r.v29_replacement_detector_seal_receipt->>'descriptor_jcs' IS DISTINCT FROM p_descriptor
 OR r.v29_replacement_detector_execution_policy IS NULL THEN RETURN NULL;END IF;
 d:=p_descriptor::jsonb;c:=r.v29_replacement_detector_seal_receipt->'closure';
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(d->>'task_id')::uuid FOR SHARE;
 IF NOT FOUND OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested OR t.canonical_result_id IS NOT NULL THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO params FROM photo.backend_documents
 WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=t.id AND purpose='task_params' AND state='ready'
 AND artifact_id=t.params_artifact_id;
 IF params IS NULL THEN RETURN NULL;END IF;
 profile:=(d->>'profile_jcs')::jsonb||jsonb_build_object('contracts_sha','341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815',
 'v2_input_manifest',c->'manifest','v2_entry_count',c->'entry_count','v2_entries_sha',c->>'entries_sha',
 'runtime_lock_sha',(r.v29_replacement_detector_execution_policy::jsonb)->>'runtime_lock_sha',
 'execution_policy_sha',encode(sha256(convert_to(r.v29_replacement_detector_execution_policy,'UTF8')),'hex'));
 IF t.inputs IS DISTINCT FROM jsonb_build_array(c->'manifest',params) OR t.execution_profile IS DISTINCT FROM profile
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN r.v29_replacement_detector_seal_receipt;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_replacement_detector_seal_receipt(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_replacement_detector_seal_receipt(uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_replacement_detector_seal_receipt(uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
