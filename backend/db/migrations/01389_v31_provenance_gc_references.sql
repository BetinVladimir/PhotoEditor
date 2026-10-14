-- +goose Up
-- Retain signed bytes, C2PA container and original encoded source while the
-- immutable provenance record exists. Adoption must independently verify C2PA.
CREATE TRIGGER ready_artifact_reference BEFORE INSERT ON photo.provenance_manifests
 FOR EACH ROW EXECUTE FUNCTION photo.guard_ready_artifact_reference('manifest_artifact_id','signed_artifact_id');
-- +goose StatementBegin
CREATE FUNCTION photo.guard_provenance_export_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.exports%ROWTYPE;source photo.artifacts%ROWTYPE;signed photo.artifacts%ROWTYPE;manifest photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO e FROM photo.exports WHERE tenant_id=NEW.tenant_id AND id=NEW.export_id FOR SHARE;
 IF NOT FOUND OR e.status IS DISTINCT FROM 'ready' OR e.artifact_id IS NULL THEN
 RAISE EXCEPTION 'PROVENANCE_EXPORT_NOT_READY' USING ERRCODE='23514';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 WHERE v.tenant_id=NEW.tenant_id AND v.id=e.asset_version_id AND a.project_id=NEW.project_id AND a.deleted_at IS NULL) THEN
 RAISE EXCEPTION 'PROVENANCE_EXPORT_PROJECT_MISMATCH' USING ERRCODE='23514';END IF;
 SELECT * INTO source FROM photo.artifacts WHERE tenant_id=NEW.tenant_id AND id=e.artifact_id FOR SHARE;
 SELECT * INTO signed FROM photo.artifacts WHERE tenant_id=NEW.tenant_id AND id=NEW.signed_artifact_id FOR SHARE;
 SELECT * INTO manifest FROM photo.artifacts WHERE tenant_id=NEW.tenant_id AND id=NEW.manifest_artifact_id FOR SHARE;
 IF source.id IS NULL OR signed.id IS NULL OR manifest.id IS NULL
 OR source.state IS DISTINCT FROM 'ready' OR signed.state IS DISTINCT FROM 'ready' OR manifest.state IS DISTINCT FROM 'ready'
 OR source.verified_at IS NULL OR signed.verified_at IS NULL OR manifest.verified_at IS NULL
 OR coalesce(source.object_version,'') IN('','null') OR coalesce(signed.object_version,'') IN('','null') OR coalesce(manifest.object_version,'') IN('','null')
 OR source.media_type NOT IN('image/jpeg','image/png') OR signed.media_type IS DISTINCT FROM source.media_type
 OR manifest.media_type='application/json' OR source.id= signed.id OR source.id=manifest.id
 OR source.bytes<1 OR signed.bytes<1 OR manifest.bytes<1
 OR (source.expires_at IS NOT NULL AND source.expires_at<=clock_timestamp())
 OR (signed.expires_at IS NOT NULL AND signed.expires_at<=clock_timestamp())
 OR (manifest.expires_at IS NOT NULL AND manifest.expires_at<=clock_timestamp()) THEN
 RAISE EXCEPTION 'PROVENANCE_ARTIFACT_NOT_READY' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_provenance_export_reference() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_provenance_export_reference() FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
CREATE TRIGGER ready_provenance_export BEFORE INSERT ON photo.provenance_manifests
 FOR EACH ROW EXECUTE FUNCTION photo.guard_provenance_export_reference();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT
  EXISTS(SELECT FROM photo.provenance_manifests m WHERE m.tenant_id=p_tenant AND p_artifact IN(m.manifest_artifact_id,m.signed_artifact_id))
  OR EXISTS(SELECT FROM photo.provenance_manifests m JOIN photo.exports e ON e.tenant_id=m.tenant_id AND e.id=m.export_id WHERE m.tenant_id=p_tenant AND e.artifact_id=p_artifact)
  OR
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
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
