-- +goose Up
-- Compose shell and quota in one statement, so quota refusal rolls back all
-- draft and target mutations. Caller must still register/seal/activate and finish
-- parent/idempotency/events in the same transaction. Private; no dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.create_semantic_shown_apply_reserved_draft(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
 p_revision bigint,p_request_raw text,p_artifact uuid,p_body text,p_documents jsonb,p_child uuid,p_tasks jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE batch uuid;project uuid;reserved bigint;
BEGIN
 batch:=photo.create_semantic_shown_apply_draft_shell(p_tenant,p_intent,p_actor,p_session,p_revision,
 p_request_raw,p_artifact,p_body,p_documents,p_child,p_tasks);
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_INTENT_UNAVAILABLE' USING ERRCODE='42501';END IF;
 reserved:=photo.reserve_semantic_shown_apply_quota(p_tenant,project,p_actor,p_child,p_tasks);
 IF reserved IS NULL OR reserved<1 THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_QUOTA_UNAVAILABLE' USING ERRCODE='23514';END IF;
 RETURN batch;
END $$;
ALTER FUNCTION photo.create_semantic_shown_apply_reserved_draft(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_semantic_shown_apply_reserved_draft(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
