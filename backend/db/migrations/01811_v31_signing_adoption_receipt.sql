-- +goose Up
-- Preserve the exact adopted task/result and physical verification digest for
-- future Delivery dependency checks. Historical rows remain distinguishable
-- and cannot masquerade as rows with this receipt.
ALTER TABLE photo.provenance_manifests
 ADD COLUMN adopted_task_id uuid,
 ADD COLUMN adopted_result_id uuid,
 ADD COLUMN validation_sha text,
 ADD CONSTRAINT provenance_adoption_receipt_complete CHECK (
  (adopted_task_id IS NULL AND adopted_result_id IS NULL AND validation_sha IS NULL)
  OR (adopted_task_id IS NOT NULL AND adopted_result_id IS NOT NULL
   AND validation_sha ~ '^[0-9a-f]{64}$'));

DROP TRIGGER immutable_content ON photo.provenance_manifests;
CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.provenance_manifests FOR EACH ROW
 EXECUTE FUNCTION photo.reject_content_update('id','tenant_id','project_id','created_at','export_id','workflow_id',
  'manifest_artifact_id','signed_artifact_id','signer_key_id','signing_policy_sha',
  'adopted_task_id','adopted_result_id','validation_sha');

-- +goose StatementBegin
DO $$ DECLARE definition text;old_insert text;new_insert text;old_values text;new_values text;
BEGIN
 SELECT pg_get_functiondef('photo.adopt_v31_signed(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text)'::regprocedure) INTO definition;
 old_insert:='INSERT INTO photo.provenance_manifests(tenant_id,project_id,export_id,workflow_id,manifest_artifact_id,signed_artifact_id,signer_key_id,signing_policy_sha)';
 new_insert:='INSERT INTO photo.provenance_manifests(tenant_id,project_id,export_id,workflow_id,manifest_artifact_id,signed_artifact_id,signer_key_id,signing_policy_sha,adopted_task_id,adopted_result_id,validation_sha)';
 old_values:='VALUES(p_tenant,w.project_id,selected_export,w.id,manifest_id,signed_id,policy->>''key_handle'',params->''policy''->>''sha256'');';
 new_values:='VALUES(p_tenant,w.project_id,selected_export,w.id,manifest_id,signed_id,policy->>''key_handle'',params->''policy''->>''sha256'',p_task,p_result,p_validation_sha);';
 IF definition IS NULL OR position(old_insert IN definition)=0 OR position(old_values IN definition)=0
  OR position('IF photo.adopt_v2_plan_pre_v31_signer(' IN definition)=0
 THEN RAISE EXCEPTION 'SIGNING_ADOPTION_RECEIPT_SHAPE';END IF;
 definition:=replace(definition,old_insert,new_insert);
 definition:=replace(definition,old_values,new_values);
 EXECUTE definition;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
