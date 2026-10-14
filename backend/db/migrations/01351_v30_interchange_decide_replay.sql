-- +goose Up
-- Historical receipt replay still requires current editor authorization.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_decide_replay(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid,p_key text,p_sha text)
RETURNS TABLE(outcome text,response_body jsonb) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;receipt photo.idempotency_records%ROWTYPE;owner uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128
 OR p_key !~ '^[!-~]+$' OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'INTERCHANGE_DECISION_REPLAY_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange';
 IF w.id IS NULL OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'INTERCHANGE_DECISION_FORBIDDEN' USING ERRCODE='42501';END IF;
 SELECT * INTO receipt FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND route='Interchange.DecideImport' AND key=p_key;
 IF receipt.id IS NULL THEN RETURN QUERY SELECT 'absent'::text,NULL::jsonb;RETURN;END IF;
 IF receipt.request_hash IS DISTINCT FROM p_sha THEN RETURN QUERY SELECT 'conflict'::text,NULL::jsonb;RETURN;END IF;
 IF receipt.response_status IS NULL THEN RETURN QUERY SELECT 'pending'::text,NULL::jsonb;RETURN;END IF;
 owner:=photo.v30_uuid5(p_workflow,'interchange:review:decide:'||p_actor::text||':'||p_sha);
 IF receipt.response_status<>200 OR receipt.resource_id IS DISTINCT FROM owner
 OR receipt.response_body->>'workflow_id' IS DISTINCT FROM w.id::text OR receipt.response_body->>'batch_id' IS DISTINCT FROM owner::text
 OR receipt.response_body->'child' IS DISTINCT FROM 'null'::jsonb
 OR coalesce(receipt.response_body->>'revision','') !~ '^[1-9][0-9]{0,15}$'
 OR coalesce(receipt.response_body->>'remaining','') !~ '^[0-9]{1,4}$'
 OR (receipt.response_body->>'remaining')::integer NOT BETWEEN 0 AND 5000
 OR NOT EXISTS(SELECT 1 FROM photo.v2_review_batches WHERE tenant_id=p_tenant AND id=owner AND project_id=w.project_id AND workflow_id=w.id AND actor_id=p_actor AND action='decide' AND child_run_id IS NULL)
 THEN RETURN QUERY SELECT 'conflict'::text,NULL::jsonb;RETURN;END IF;
 RETURN QUERY SELECT 'replayed'::text,receipt.response_body;
END $$;
ALTER FUNCTION photo.v30_interchange_decide_replay(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_decide_replay(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_decide_replay(uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
