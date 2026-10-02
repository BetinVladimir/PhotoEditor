-- Pin live identity grants into the learning workflow before measurable
-- holdout identity evidence is exposed to the evaluate task. Revocation then
-- cancels the linked run through the existing consent lifecycle.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.guard_v22_workflow_consent() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE current_grant photo.consent_grants%ROWTYPE;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.runs r
  ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  WHERE w.tenant_id=NEW.tenant_id AND w.project_id=NEW.project_id
   AND w.id=NEW.workflow_id AND r.status='draft' AND NOT r.graph_sealed)
  AND NOT EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.runs r
   ON r.tenant_id=w.tenant_id AND r.id=w.run_id
   JOIN photo.graph_expansions e ON e.tenant_id=r.tenant_id
    AND e.run_id=r.id
   WHERE w.tenant_id=NEW.tenant_id AND w.project_id=NEW.project_id
    AND w.id=NEW.workflow_id AND w.workflow_type='learning'
    AND r.status='running' AND NOT r.graph_sealed AND
    NOT r.cancel_requested AND e.kind='v2.learning.evaluate/1' AND
    e.state='open' AND e.expansion_token IS NOT NULL AND
    e.expansion_lease_until>clock_timestamp()) THEN
  RAISE EXCEPTION 'V22_CONSENT_LINK_TOO_LATE' USING ERRCODE='23514';
 END IF;
 SELECT * INTO current_grant FROM photo.consent_grants g
  WHERE g.tenant_id=NEW.tenant_id AND g.project_id=NEW.project_id
   AND g.id=NEW.consent_id FOR SHARE;
 IF current_grant.id IS NULL OR current_grant.status<>'granted'
  OR current_grant.epoch<>NEW.consent_epoch THEN
  RAISE EXCEPTION 'V22_CONSENT_EPOCH_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v22_workflow_consent() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v22_workflow_consent() FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_pin_evaluate_identity_consents(p_tenant uuid,
 p_run uuid,p_expansion uuid,p_token uuid,p_body jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE workflow uuid; project uuid; grant_item jsonb;
 grant_row photo.consent_grants%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR jsonb_typeof(p_body) IS DISTINCT FROM 'object' OR
  jsonb_typeof(p_body->'grants') IS DISTINCT FROM 'array' OR
  jsonb_array_length(p_body->'grants')>5000 THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT w.id,w.project_id INTO workflow,project FROM photo.graph_expansions e
  JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
  JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  JOIN photo.tasks fit ON fit.tenant_id=e.tenant_id AND fit.id=e.parent_task_id
  WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_expansion
   AND e.kind='v2.learning.evaluate/1' AND e.state='open'
   AND e.expansion_token=p_token AND
   e.expansion_lease_until>clock_timestamp()
   AND r.kind='v2' AND r.status='running' AND NOT r.graph_sealed
   AND NOT r.cancel_requested AND w.workflow_type='learning'
   AND fit.operation='learning.fit' AND fit.operation_version='1'
   AND fit.state='succeeded' AND fit.canonical_result_id IS NOT NULL;
 IF workflow IS NULL THEN RETURN false; END IF;
 FOR grant_item IN SELECT value FROM jsonb_array_elements(p_body->'grants') LOOP
  IF grant_item->>'purpose' IS DISTINCT FROM 'identity_matching' OR
   grant_item->>'consent_id' IS NULL OR
   grant_item->>'epoch' IS NULL OR
   grant_item->>'person_id' IS NULL THEN RETURN false; END IF;
  SELECT * INTO grant_row FROM photo.consent_grants g
   WHERE g.tenant_id=p_tenant AND g.project_id=project
    AND g.id=(grant_item->>'consent_id')::uuid FOR SHARE;
  IF grant_row.id IS NULL OR grant_row.status<>'granted' OR
   grant_row.epoch::text<>grant_item->>'epoch' OR
   grant_row.purpose<>'identity_matching' OR
   grant_row.person_id::text IS DISTINCT FROM grant_item->>'person_id' OR
   NOT EXISTS(SELECT 1 FROM photo.project_persons person
    WHERE person.tenant_id=p_tenant AND person.project_id=project
     AND person.id=grant_row.person_id AND person.state='active')
   THEN RETURN false; END IF;
 END LOOP;
 FOR grant_item IN SELECT value FROM jsonb_array_elements(p_body->'grants') LOOP
  SELECT * INTO grant_row FROM photo.consent_grants g
   WHERE g.tenant_id=p_tenant AND g.project_id=project
    AND g.id=(grant_item->>'consent_id')::uuid FOR SHARE;
  INSERT INTO photo.workflow_consent_links(tenant_id,project_id,workflow_id,
   consent_id,consent_epoch)
   VALUES(p_tenant,project,workflow,grant_row.id,grant_row.epoch)
   ON CONFLICT (tenant_id,workflow_id,consent_id) DO NOTHING;
  IF NOT EXISTS(SELECT 1 FROM photo.workflow_consent_links link
   WHERE link.tenant_id=p_tenant AND link.project_id=project
    AND link.workflow_id=workflow AND link.consent_id=grant_row.id
    AND link.consent_epoch=grant_row.epoch) THEN
   RAISE EXCEPTION 'V23_IDENTITY_CONSENT_PIN_CONFLICT' USING ERRCODE='23514';
  END IF;
 END LOOP;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_pin_evaluate_identity_consents(uuid,uuid,uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_pin_evaluate_identity_consents(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_pin_evaluate_identity_consents(uuid,uuid,uuid,uuid,jsonb)
 TO photo_scheduler;
