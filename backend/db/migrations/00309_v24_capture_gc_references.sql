-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT
  EXISTS(SELECT FROM photo.v2_documents x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_document_refs x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_artifact_links x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_task_inputs x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_document_approvals x WHERE x.tenant_id=p_tenant AND x.resolved_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_review_batches x WHERE x.tenant_id=p_tenant AND x.decisions_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.model_qualifications x WHERE x.tenant_id=p_tenant AND x.report_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.session_analyses x WHERE x.tenant_id=p_tenant AND x.plan_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.person_observations x WHERE x.tenant_id=p_tenant AND x.evidence_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.learning_versions x WHERE x.tenant_id=p_tenant AND p_artifact IN(x.training_artifact_id,x.model_artifact_id,x.evaluation_artifact_id))
  OR EXISTS(SELECT FROM photo.consent_cleanup x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.artifact_id=p_artifact AND x.state='ready')
  OR EXISTS(SELECT FROM photo.capture_alerts x WHERE x.tenant_id=p_tenant AND x.report_artifact_id=p_artifact)
  OR EXISTS(SELECT FROM photo.shoot_sessions x WHERE x.tenant_id=p_tenant AND (x.policy->'calibration'->>'artifact_id'=p_artifact::text OR x.capture_policy->'calibration'->>'artifact_id'=p_artifact::text))
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_artifact_has_references(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_artifact_has_references(uuid,uuid) FROM PUBLIC;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
