-- +goose Up
-- Admission receipt is stored in runs and follows the existing tenant purge.
ALTER TABLE photo.runs ADD COLUMN v28_donor_queued_receipt jsonb
 CHECK(v28_donor_queued_receipt IS NULL OR (jsonb_typeof(v28_donor_queued_receipt)='object' AND pg_column_size(v28_donor_queued_receipt)<=65536));
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v28_donor_graph(uuid,uuid,uuid,uuid,jsonb) RENAME TO seal_v28_donor_graph_pre_receipt;
REVOKE ALL ON FUNCTION photo.seal_v28_donor_graph_pre_receipt(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v28_donor_graph(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_manual jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;workflow_id uuid;plan jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v28_donor_queued_receipt IS NOT NULL THEN RETURN false;END IF;
 SELECT id INTO workflow_id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
 AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF photo.seal_v28_donor_graph_pre_receipt(p_tenant,p_run,p_actor,p_session,p_manual) IS DISTINCT FROM true THEN RETURN false;END IF;
 plan:=r.v28_donor_descriptor_jcs::jsonb;
 UPDATE photo.runs SET v28_donor_queued_receipt=jsonb_build_object('schema_version',1,
 'run_id',p_run,'workflow_id',workflow_id,'task_id',plan->'task'->>'id',
 'actor_id',p_actor,'auth_session_id',p_session,'state','queued',
 'descriptor_sha',encode(sha256(convert_to(r.v28_donor_descriptor_jcs,'UTF8')),'hex'),
 'manual',p_manual,'qualification',plan->'task'->'execution_profile'->'donor_qualification')
 WHERE tenant_id=p_tenant AND id=p_run AND graph_sealed AND v28_donor_queued_receipt IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'V28_DONOR_RECEIPT_RACE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v28_donor_graph(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v28_donor_graph(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v28_donor_graph(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
