-- +goose Up
-- Bare refusals did not prove absence of a committed child. Retain their audit
-- evidence, then restore exact-key recovery from the unchanged frozen plan.
WITH retracted AS (
 DELETE FROM photo.export_delivery_tickets
 WHERE ticket IS NULL AND failure_code IS NOT NULL
 RETURNING tenant_id,batch_id,group_key,failure_code
)
INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
SELECT tenant_id,'system',NULL,'export.admission_refusal_retracted','export_delivery_batch',batch_id,gen_random_uuid(),
 jsonb_build_object('migration','02534','group_key',group_key,'previous_failure_code',failure_code,
 'reason','error_code_did_not_prove_absence_of_committed_child') FROM retracted;
-- Old binaries must not silently recreate sticky failure rows during rollout.
-- Completed/accepted tickets and immutable plans are untouched.
ALTER TABLE photo.export_delivery_tickets ADD CONSTRAINT export_delivery_admission_requires_ticket
 CHECK(failure_code IS NULL);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
