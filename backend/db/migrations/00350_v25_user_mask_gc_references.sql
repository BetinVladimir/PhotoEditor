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
  OR EXISTS(SELECT 1 FROM photo.v2_draft_uploads u
   JOIN photo.artifacts root ON root.tenant_id=u.tenant_id AND root.id=u.artifact_id
   WHERE u.tenant_id=p_tenant AND u.user_mask_binding IS NOT NULL
    AND u.state IN('ready','retired') AND root.state='ready'
    AND (u.user_mask_binding->'source'->>'artifact_id'=p_artifact::text
      OR u.user_mask_binding->'mask'->'artifact'->>'artifact_id'=p_artifact::text)
    AND (  EXISTS(SELECT FROM photo.v2_documents r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_document_refs r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_artifact_links r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_task_inputs r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_document_approvals r WHERE r.tenant_id=p_tenant AND r.resolved_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_review_batches r WHERE r.tenant_id=p_tenant AND r.decisions_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.model_qualifications r WHERE r.tenant_id=p_tenant AND r.report_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.session_analyses r WHERE r.tenant_id=p_tenant AND r.plan_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.person_observations r WHERE r.tenant_id=p_tenant AND r.evidence_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.learning_versions r WHERE r.tenant_id=p_tenant AND u.artifact_id IN(r.training_artifact_id,r.model_artifact_id,r.evaluation_artifact_id))
  OR EXISTS(SELECT FROM photo.consent_cleanup r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.v2_draft_uploads r WHERE r.tenant_id=p_tenant AND r.artifact_id=u.artifact_id AND r.state='ready')
  OR EXISTS(SELECT FROM photo.capture_alerts r WHERE r.tenant_id=p_tenant AND r.report_artifact_id=u.artifact_id)
  OR EXISTS(SELECT FROM photo.shoot_sessions r WHERE r.tenant_id=p_tenant AND (r.policy->'calibration'->>'artifact_id'=u.artifact_id::text OR r.capture_policy->'calibration'->>'artifact_id'=u.artifact_id::text))))

$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_artifact_has_references(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_artifact_has_references(uuid,uuid) FROM PUBLIC;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
