-- +goose Up
-- Recovery proves dedicated adoption; only an actually completed run is reported
-- completed. No pending-ledger expiry bypass grants object download locations.
-- +goose StatementBegin
CREATE FUNCTION photo.recover_v30_interchange_export(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;wid uuid;tid uuid;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;t photo.tasks%ROWTYPE;
 proof photo.v30_interchange_export_evidence%ROWTYPE;e jsonb;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 120
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');tid:=photo.v30_uuid5(wid,'v2.interchange.export:0');
 -- Tenant-first locking is shared with adoption/finalization/purge.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=wid AND run_id=rid;
 IF r.id IS NULL OR w.id IS NULL OR r.created_by IS DISTINCT FROM p_actor OR r.config_snapshot->>'request_sha' IS DISTINCT FROM p_request_sha
 OR photo.v25_lens_snapshot_ready(p_tenant,w.project_id,w.input_snapshot_id,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id AND archived_at IS NULL AND download_allowed)
 OR photo.v30_interchange_export_committed(p_tenant,rid) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid AND id=tid;
 SELECT * INTO proof FROM photo.v30_interchange_export_evidence WHERE tenant_id=p_tenant AND run_id=rid AND result_id=t.canonical_result_id AND request_sha=p_request_sha;
 IF proof.run_id IS NULL THEN RETURN NULL;END IF;e:=proof.evidence_jcs::jsonb;
 RETURN jsonb_build_object('schema_version',1,'workflow_id',wid,'run_id',rid,'task_id',tid,'result_id',t.canonical_result_id,
 'status',CASE WHEN r.status='completed' THEN 'completed' ELSE 'adopted' END,
 'root',e->'root','report',e->'documents'->1,'bundle',e->'bundle','evidence_sha256',proof.evidence_sha);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.recover_v30_interchange_export(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.recover_v30_interchange_export(uuid,uuid,uuid,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.recover_v30_interchange_export(uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
