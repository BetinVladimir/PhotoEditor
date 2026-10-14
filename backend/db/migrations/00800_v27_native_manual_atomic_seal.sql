-- +goose Up
-- Complete original manual evidence is locked before either activation or receipt replay.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_semantic_native_manual_preparation(p_tenant uuid,p_run uuid,p_packet jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ticket jsonb;
BEGIN
 IF photo.lock_semantic_native_manual_admission_evidence(p_tenant,p_run,p_packet) IS NOT TRUE THEN RETURN NULL;END IF;
 -- Inner native seal repeats its own source/qualification/registration checks.
 -- Manual locks and person-inventory writer barrier remain held in this transaction.
 ticket:=photo.seal_semantic_native_defect_preparation(p_tenant,p_run,p_packet->'native');
 IF ticket IS NULL THEN RETURN NULL;END IF;
 IF ticket->>'run_id' IS DISTINCT FROM p_run::text OR ticket->>'state' IS DISTINCT FROM 'queued' THEN
  RAISE EXCEPTION 'V27_MANUAL_TICKET_MISMATCH' USING ERRCODE='23514';
 END IF;
 RETURN ticket;
END $$;
ALTER FUNCTION photo.seal_semantic_native_manual_preparation(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_native_manual_preparation(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_semantic_native_manual_preparation(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
