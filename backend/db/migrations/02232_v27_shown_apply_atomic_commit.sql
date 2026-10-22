-- +goose Up
-- Only complete mutation entry point: receipt lock -> source/decision/preview
-- locks -> draft/quota -> registration/seal -> activation/parent/receipt/events.
-- Any refusal raises and rolls back the whole statement. Completed idempotency
-- replay is handled by the API before entering this new-mutation function.
-- +goose StatementBegin
CREATE FUNCTION photo.commit_semantic_shown_apply_review(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
 p_key text,p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb,p_child uuid,p_tasks jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE;batch uuid;project uuid;sealed integer;response jsonb;
BEGIN
 IF p_request_raw IS NULL OR octet_length(p_request_raw) NOT BETWEEN 2 AND 65536
 OR p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9007199254740990 THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_COMMIT_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO idem FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user'
 AND principal_id=p_actor AND route='SemanticIntent.Apply' AND key=p_key FOR UPDATE;
 IF NOT FOUND OR idem.request_hash IS DISTINCT FROM encode(sha256(convert_to(p_request_raw,'UTF8')),'hex')
 OR idem.response_status IS NOT NULL THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_RECEIPT_CONFLICT' USING ERRCODE='40001';END IF;
 batch:=photo.create_semantic_shown_apply_reserved_draft(p_tenant,p_intent,p_actor,p_session,p_revision,
 p_request_raw,p_artifact,p_body,p_documents,p_child,p_tasks);
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_INTENT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 sealed:=photo.seal_semantic_shown_apply_inputs(p_tenant,project,p_actor,p_child,p_tasks);
 IF sealed IS DISTINCT FROM jsonb_array_length(p_tasks) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_SEAL_INCOMPLETE' USING ERRCODE='23514';END IF;
 response:=photo.finish_semantic_shown_apply_review(p_tenant,p_intent,p_actor,p_session,p_key,p_revision,
 p_request_raw,p_artifact,p_body,p_child,p_tasks);
 IF response->>'batch_id' IS DISTINCT FROM batch::text OR response->'child'->>'run_id' IS DISTINCT FROM p_child::text THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_RECEIPT_INVALID' USING ERRCODE='23514';END IF;
 RETURN response;
END $$;
ALTER FUNCTION photo.commit_semantic_shown_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_semantic_shown_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.commit_semantic_shown_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb)
 TO photo_semantic_admission;
-- +goose StatementEnd
