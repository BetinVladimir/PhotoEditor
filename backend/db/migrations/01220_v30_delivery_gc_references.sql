-- V30 delivery references participate in the existing GC predicate.
-- +goose Up
CREATE INDEX delivery_policy_reference ON photo.delivery_packages(tenant_id,policy_artifact_id);
CREATE INDEX delivery_manifest_reference ON photo.delivery_packages(tenant_id,manifest_artifact_id) WHERE manifest_artifact_id IS NOT NULL;
CREATE TRIGGER ready_artifact_reference BEFORE INSERT OR UPDATE ON photo.delivery_packages
 FOR EACH ROW EXECUTE FUNCTION photo.guard_ready_artifact_reference('policy_artifact_id','manifest_artifact_id');
-- Serialize export attachment with GC detach; full project/source authority
-- remains the responsibility of future scoped delivery adoption functions.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_delivery_export_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.exports%ROWTYPE;a photo.artifacts%ROWTYPE;
BEGIN
 IF NEW.export_id IS NULL THEN RETURN NEW;END IF;
 SELECT * INTO e FROM photo.exports WHERE tenant_id=NEW.tenant_id AND id=NEW.export_id FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_EXPORT_MISSING' USING ERRCODE='23503';END IF;
 IF NEW.state='ready' AND (e.status<>'ready' OR e.artifact_id IS NULL) THEN
  RAISE EXCEPTION 'DELIVERY_EXPORT_NOT_READY' USING ERRCODE='23514';END IF;
 IF e.artifact_id IS NOT NULL THEN
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=NEW.tenant_id AND id=e.artifact_id FOR SHARE;
  IF NOT FOUND OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
   OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN
   RAISE EXCEPTION 'DELIVERY_EXPORT_ARTIFACT_NOT_READY' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_delivery_export_reference() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_delivery_export_reference() FROM PUBLIC;
CREATE TRIGGER ready_delivery_export BEFORE INSERT OR UPDATE ON photo.delivery_items
 FOR EACH ROW EXECUTE FUNCTION photo.guard_delivery_export_reference();
-- Keep references while relational rows exist, including failed/cancelled audit
-- packages. Explicit lifecycle detach/purge must precede artifact collection.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT
  EXISTS(SELECT FROM photo.delivery_packages d WHERE d.tenant_id=p_tenant AND p_artifact IN(d.policy_artifact_id,d.manifest_artifact_id))
  OR EXISTS(SELECT FROM photo.delivery_items d JOIN photo.exports e ON e.tenant_id=d.tenant_id AND e.id=d.export_id WHERE d.tenant_id=p_tenant AND e.artifact_id=p_artifact)
  OR
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

  OR EXISTS(SELECT 1 FROM photo.semantic_native_protections n
   JOIN photo.artifacts root ON root.tenant_id=n.tenant_id AND root.id=n.root_id
   WHERE n.tenant_id=p_tenant AND root.state='ready'
    AND p_artifact IN(n.pixels_id,n.report_id,n.source_id)
    AND (EXISTS(SELECT FROM photo.v2_documents x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_document_refs x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_artifact_links x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_task_inputs x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_document_approvals x WHERE x.tenant_id=p_tenant AND x.resolved_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_review_batches x WHERE x.tenant_id=p_tenant AND x.decisions_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.model_qualifications x WHERE x.tenant_id=p_tenant AND x.report_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.session_analyses x WHERE x.tenant_id=p_tenant AND x.plan_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.person_observations x WHERE x.tenant_id=p_tenant AND x.evidence_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.learning_versions x WHERE x.tenant_id=p_tenant AND n.root_id IN(x.training_artifact_id,x.model_artifact_id,x.evaluation_artifact_id))
  OR EXISTS(SELECT FROM photo.consent_cleanup x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.artifact_id=n.root_id AND x.state='ready')
  OR EXISTS(SELECT FROM photo.capture_alerts x WHERE x.tenant_id=p_tenant AND x.report_artifact_id=n.root_id)
  OR EXISTS(SELECT FROM photo.shoot_sessions x WHERE x.tenant_id=p_tenant AND (x.policy->'calibration'->>'artifact_id'=n.root_id::text OR x.capture_policy->'calibration'->>'artifact_id'=n.root_id::text))))

$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_artifact_has_references(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_artifact_has_references(uuid,uuid) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
