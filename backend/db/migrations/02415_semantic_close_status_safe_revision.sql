-- +goose Up
-- Advisory UI fence; admission still locks and rechecks all source/target state.
-- No object keys, versions or caller-supplied principal are exposed over HTTP.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_review_status(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(run_id uuid,workflow_id uuid,revision text,state text,plan_sha text,remaining integer,can_close boolean)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE identity record;
BEGIN
 SELECT * INTO identity FROM photo.semantic_review_identity(p_tenant,p_intent,p_actor,p_session);
 RETURN QUERY
 SELECT r.id,identity.workflow_id,r.revision::text,r.status,plan.sha256,
 (SELECT count(*)::integer FROM photo.semantic_targets t WHERE t.tenant_id=p_tenant AND t.intent_id=p_intent AND t.state='proposed'),
 r.status='waiting_review' AND NOT r.cancel_requested AND r.revision<9007199254740991 AND plan.sha256 IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM photo.semantic_targets t WHERE t.tenant_id=p_tenant AND t.intent_id=p_intent AND t.state='proposed')
 FROM photo.runs r LEFT JOIN LATERAL (
 SELECT source.sha256 FROM photo.semantic_review_plan_sources(p_tenant,identity.workflow_id,p_actor,p_session) source
 WHERE source.contract='SemanticPlan'
 ) plan ON true WHERE r.tenant_id=p_tenant AND r.id=identity.run_id;
END $$;
ALTER FUNCTION photo.semantic_review_status(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_review_status(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_review_status(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
