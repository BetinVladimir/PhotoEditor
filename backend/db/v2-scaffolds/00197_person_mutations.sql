-- A person is local to one project. Creation does not infer an identity or
-- grant consent; both person-scoped grants start revoked at epoch one.
-- +goose StatementBegin
CREATE FUNCTION photo.create_v22_person(p_tenant uuid,p_project uuid,p_label text,
 p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE created photo.project_persons%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR p_session IS NULL
  OR length(coalesce(p_label,'')) NOT BETWEEN 1 AND 120 THEN
  RAISE EXCEPTION 'V22_PERSON_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN
  RAISE EXCEPTION 'V22_PERSON_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 INSERT INTO photo.project_persons(tenant_id,project_id,label,state,consent_epoch)
 VALUES(p_tenant,p_project,p_label,'active',1) RETURNING * INTO created;
 INSERT INTO photo.consent_grants(tenant_id,project_id,purpose,person_id,status,
  epoch,policy_version,actor_id)
 VALUES
  (p_tenant,p_project,'identity_matching',created.id,'revoked',1,'v2.2',p_actor),
  (p_tenant,p_project,'learning_person',created.id,'revoked',1,'v2.2',p_actor);
 PERFORM photo.ensure_v22_photographer_grant(p_tenant,p_project,p_actor);
 RETURN jsonb_build_object('id',created.id,'revision',created.revision::text,
  'state',created.state);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_v22_person(uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v22_person(uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v22_person(uuid,uuid,text,uuid,uuid) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.revoke_v22_person(p_tenant uuid,p_project uuid,p_person uuid,
 p_expected bigint,p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE current_row photo.project_persons%ROWTYPE; changed photo.project_persons%ROWTYPE;
 grant_row record; event_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_person IS NULL OR p_actor IS NULL
  OR p_session IS NULL OR p_expected IS NULL OR p_expected<1 THEN
  RAISE EXCEPTION 'V22_PERSON_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.authorize_tenant(
  p_actor,p_session,p_tenant) a WHERE a.role='owner') THEN
  RAISE EXCEPTION 'V22_PERSON_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT * INTO current_row FROM photo.project_persons p WHERE p.tenant_id=p_tenant
  AND p.project_id=p_project AND p.id=p_person FOR UPDATE;
 IF current_row.id IS NULL THEN
  RAISE EXCEPTION 'V22_PERSON_NOT_FOUND' USING ERRCODE='P0002';
 END IF;
 IF current_row.revision<>p_expected OR current_row.state<>'active' OR
  current_row.consent_epoch>=9223372036854775806 THEN
  RAISE EXCEPTION 'V22_PERSON_REVISION_CONFLICT' USING ERRCODE='40001';
 END IF;
 FOR grant_row IN SELECT g.id,g.revision,g.purpose FROM photo.consent_grants g
  WHERE g.tenant_id=p_tenant AND g.project_id=p_project AND g.person_id=p_person
   AND g.status='granted' ORDER BY g.id LOOP
  PERFORM photo.change_v2_consent(p_tenant,p_project,grant_row.id,
   grant_row.revision,grant_row.purpose,p_person,'revoke','v2.2',p_actor,p_session);
 END LOOP;
 UPDATE photo.project_persons SET state='revoked',consent_epoch=consent_epoch+1
  WHERE tenant_id=p_tenant AND id=p_person RETURNING * INTO changed;
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v2.person.revoked',
  jsonb_build_object('schema_version',1,'event_id',event_id,
   'type','v2.person.revoked','tenant_id',p_tenant,'project_id',p_project,
   'person_id',p_person,'consent_epoch',changed.consent_epoch));
 RETURN jsonb_build_object('id',changed.id,'revision',changed.revision::text,
  'state',changed.state,'consent_epoch',changed.consent_epoch::text);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.revoke_v22_person(uuid,uuid,uuid,bigint,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.revoke_v22_person(uuid,uuid,uuid,bigint,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.revoke_v22_person(uuid,uuid,uuid,bigint,uuid,uuid) TO photo_api;
