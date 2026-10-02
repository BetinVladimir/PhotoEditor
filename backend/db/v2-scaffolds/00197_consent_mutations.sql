-- V22 grant/revoke boundary. V23 extends the same entrypoint to disable
-- dependent learning versions and enqueue artifact cleanup before release.
-- +goose StatementBegin
CREATE FUNCTION photo.change_v2_consent(p_tenant uuid,p_project uuid,p_consent uuid,
 p_expected bigint,p_purpose text,p_person uuid,p_action text,p_policy_version text,
 p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE grant_row photo.consent_grants%ROWTYPE; changed photo.consent_grants%ROWTYPE;
 dependent record; run_row photo.runs%ROWTYPE; task_row record; next_status text;
 event_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_consent IS NULL OR p_actor IS NULL
  OR p_session IS NULL OR p_expected IS NULL OR p_expected<1 OR
  coalesce(p_action,'') NOT IN('grant','revoke') OR
  length(coalesce(p_policy_version,'')) NOT BETWEEN 1 AND 80 THEN
  RAISE EXCEPTION 'V22_CONSENT_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.authorize_tenant(
  p_actor,p_session,p_tenant) a WHERE a.role='owner') OR
  NOT EXISTS(SELECT 1 FROM photo.projects p WHERE p.tenant_id=p_tenant
   AND p.id=p_project AND p.archived_at IS NULL) THEN
  RAISE EXCEPTION 'V22_CONSENT_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT * INTO grant_row FROM photo.consent_grants g WHERE g.tenant_id=p_tenant
  AND g.project_id=p_project AND g.id=p_consent FOR UPDATE;
 IF grant_row.id IS NULL THEN
  RAISE EXCEPTION 'V22_CONSENT_NOT_FOUND' USING ERRCODE='P0002';
 END IF;
 IF grant_row.revision<>p_expected OR grant_row.purpose IS DISTINCT FROM p_purpose OR
  grant_row.person_id IS DISTINCT FROM p_person OR
  grant_row.epoch>=9223372036854775806 OR
  (p_action='grant' AND grant_row.status<>'revoked') OR
  (p_action='revoke' AND grant_row.status<>'granted') THEN
  RAISE EXCEPTION 'V22_CONSENT_REVISION_CONFLICT' USING ERRCODE='40001';
 END IF;
 IF p_action='grant' AND p_person IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM photo.project_persons p WHERE p.tenant_id=p_tenant
   AND p.project_id=p_project AND p.id=p_person AND p.state='active') THEN
  RAISE EXCEPTION 'V22_PERSON_REVOKED' USING ERRCODE='23514';
 END IF;
 next_status:=CASE WHEN p_action='grant' THEN 'granted' ELSE 'revoked' END;
 UPDATE photo.consent_grants SET status=next_status,epoch=grant_row.epoch+1,
  policy_version=p_policy_version,actor_id=p_actor WHERE id=p_consent
 RETURNING * INTO changed;
 INSERT INTO photo.consent_events(tenant_id,project_id,consent_id,epoch,action,
  actor_id,policy_version) VALUES(p_tenant,p_project,p_consent,changed.epoch,
  p_action,p_actor,p_policy_version);
 IF p_action='revoke' THEN
  FOR dependent IN SELECT DISTINCT w.run_id FROM photo.workflow_consent_links l
   JOIN photo.v2_workflows w ON w.tenant_id=l.tenant_id AND w.id=l.workflow_id
   WHERE l.tenant_id=p_tenant AND l.project_id=p_project
    AND l.consent_id=p_consent AND l.consent_epoch=grant_row.epoch
   ORDER BY w.run_id LOOP
   SELECT * INTO run_row FROM photo.runs r WHERE r.tenant_id=p_tenant
    AND r.id=dependent.run_id FOR UPDATE;
   IF run_row.status IN('draft','queued','running','waiting_review') THEN
    IF run_row.revision>=9007199254740991 THEN
     RAISE EXCEPTION 'V22_RUN_REVISION_EXHAUSTED' USING ERRCODE='22003';
    END IF;
    UPDATE photo.runs SET cancel_requested=true,
     status=CASE WHEN status='draft' THEN 'draft' ELSE 'cancelling' END,
     revision=revision+1 WHERE tenant_id=p_tenant AND id=run_row.id;
    FOR task_row IN SELECT t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant
     AND t.run_id=run_row.id AND t.state NOT IN('succeeded','failed','cancelled','skipped')
     ORDER BY t.id LOOP
     UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=p_tenant
      AND id=task_row.id;
    END LOOP;
   END IF;
  END LOOP;
 END IF;
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v2.consent.'||p_action,
  jsonb_build_object('schema_version',1,'event_id',event_id,
   'type','v2.consent.'||p_action,'tenant_id',p_tenant,'project_id',p_project,
   'consent_id',p_consent,'epoch',changed.epoch));
 RETURN jsonb_build_object('id',changed.id,'revision',changed.revision::text,
  'epoch',changed.epoch::text,'status',changed.status);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.change_v2_consent(uuid,uuid,uuid,bigint,text,uuid,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.change_v2_consent(uuid,uuid,uuid,bigint,text,uuid,text,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.change_v2_consent(uuid,uuid,uuid,bigint,text,uuid,text,text,uuid,uuid) TO photo_api;
