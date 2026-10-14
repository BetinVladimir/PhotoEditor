-- +goose Up
-- Replay reads committed decision evidence before any expiring object reads.
-- Current session/editor authorization is required even for old keys.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_apply_replay(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text)
RETURNS TABLE(outcome text,response_body jsonb)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent photo.semantic_intents%ROWTYPE; idem photo.idempotency_records%ROWTYPE; batch photo.v2_review_batches%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_intent IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$'
 OR coalesce(p_request_sha,'') !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'SEMANTIC_REPLAY_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO intent FROM photo.semantic_intents i WHERE i.tenant_id=p_tenant AND i.id=p_intent;
 IF intent.id IS NULL OR NOT photo.v2_editor_authorized(p_tenant,intent.project_id,p_actor,p_session) THEN
  RAISE EXCEPTION 'SEMANTIC_REPLAY_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT * INTO idem FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant
 AND i.principal_type='user' AND i.principal_id=p_actor AND i.route='SemanticIntent.Apply' AND i.key=p_key;
 IF idem.id IS NULL THEN RETURN QUERY SELECT 'absent'::text,NULL::jsonb;RETURN;END IF;
 IF idem.request_hash IS DISTINCT FROM p_request_sha THEN RETURN QUERY SELECT 'conflict'::text,NULL::jsonb;RETURN;END IF;
 IF idem.response_status IS NULL THEN RETURN QUERY SELECT 'pending'::text,NULL::jsonb;RETURN;END IF;
 SELECT * INTO batch FROM photo.v2_review_batches b WHERE b.tenant_id=p_tenant
 AND b.project_id=intent.project_id AND b.workflow_id=intent.workflow_id AND b.actor_id=p_actor
 AND b.id::text=idem.response_body->>'batch_id' AND b.action IN('apply','decide');
 IF batch.id IS NULL OR idem.response_status<>200 OR idem.resource_id IS DISTINCT FROM batch.id
 OR idem.response_body->>'workflow_id' IS DISTINCT FROM intent.workflow_id::text
 OR (batch.child_run_id IS NULL AND idem.response_body->'child' IS DISTINCT FROM 'null'::jsonb)
 OR (batch.child_run_id IS NOT NULL AND idem.response_body->'child'->>'run_id' IS DISTINCT FROM batch.child_run_id::text) THEN
  RETURN QUERY SELECT 'conflict'::text,NULL::jsonb;RETURN;
 END IF;
 RETURN QUERY SELECT 'replayed'::text,idem.response_body;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.semantic_apply_replay(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_apply_replay(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_apply_replay(uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
