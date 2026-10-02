-- Pin the exact consent document and its grant epochs before object upload.
-- This is one transaction: a later revoke locks the same tenant/grant rows
-- and cancels linked workflows before any task can use stale identity data.
-- +goose StatementBegin
CREATE FUNCTION photo.pin_v22_analysis_consents(p_tenant uuid,p_run uuid,
 p_body jsonb,p_sha text,p_bytes bigint,p_bucket text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE workflow uuid; project uuid; grant_item jsonb; grant_row photo.consent_grants%ROWTYPE;
 reservation record;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_sha IS NULL OR
  p_sha !~ '^[0-9a-f]{64}$' OR p_bytes NOT BETWEEN 2 AND 8388608 OR
  jsonb_typeof(p_body) IS DISTINCT FROM 'object' OR
  p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT w.id,w.project_id INTO workflow,project FROM photo.v2_workflows w
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  WHERE w.tenant_id=p_tenant AND w.run_id=p_run
   AND w.workflow_type='session' AND w.renderer_version='v2.2'
   AND r.kind='v2' AND r.status='draft' AND NOT r.cancel_requested
   AND NOT r.graph_sealed;
 IF workflow IS NULL OR
  p_body IS DISTINCT FROM photo.v22_analysis_consent_sources(p_tenant,p_run)
  THEN RETURN false; END IF;
 FOR grant_item IN SELECT value FROM jsonb_array_elements(p_body->'grants') LOOP
  SELECT * INTO grant_row FROM photo.consent_grants g
   WHERE g.tenant_id=p_tenant AND g.project_id=project
    AND g.id=(grant_item->>'consent_id')::uuid FOR SHARE;
  IF grant_row.id IS NULL OR grant_row.status<>'granted' OR
   grant_row.epoch::text<>grant_item->>'epoch' THEN RETURN false; END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM photo.workflow_consent_links link
  WHERE link.tenant_id=p_tenant AND link.workflow_id=workflow
   AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_body->'grants') item
    WHERE item->>'consent_id'=link.consent_id::text
     AND item->>'epoch'=link.consent_epoch::text)) THEN RETURN false; END IF;
 SELECT * INTO reservation FROM photo.reserve_v22_analysis_document(
  p_tenant,p_run,p_run,'v2_analysis_consents',p_sha,p_bytes,p_bucket);
 IF reservation.outcome NOT IN ('created','resumed','replayed') THEN RETURN false; END IF;
 FOR grant_item IN SELECT value FROM jsonb_array_elements(p_body->'grants') LOOP
  INSERT INTO photo.workflow_consent_links(tenant_id,project_id,workflow_id,
   consent_id,consent_epoch)
  VALUES(p_tenant,project,workflow,(grant_item->>'consent_id')::uuid,
   (grant_item->>'epoch')::bigint)
  ON CONFLICT (tenant_id,workflow_id,consent_id) DO NOTHING;
  IF NOT EXISTS(SELECT 1 FROM photo.workflow_consent_links link
   WHERE link.tenant_id=p_tenant AND link.workflow_id=workflow
    AND link.consent_id=(grant_item->>'consent_id')::uuid
    AND link.consent_epoch=(grant_item->>'epoch')::bigint) THEN
   RAISE EXCEPTION 'V22_CONSENT_PIN_CONFLICT' USING ERRCODE='23514';
  END IF;
 END LOOP;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pin_v22_analysis_consents(uuid,uuid,jsonb,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pin_v22_analysis_consents(uuid,uuid,jsonb,text,bigint,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.pin_v22_analysis_consents(uuid,uuid,jsonb,text,bigint,text)
 TO photo_scheduler;
