-- +goose Up
-- Pin exactly the ConsentSnapshot registered for a blocked identity.compare
-- task. Existing V22 revocation then cancels the run; seal_v2_graph repeats
-- the epoch check before any task becomes runnable.
-- +goose StatementBegin
CREATE FUNCTION photo.pin_v23_identity_compare_consents(
 p_tenant uuid,p_workflow uuid,p_task uuid,p_body text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid;run_id uuid;body jsonb;grant_item jsonb;
 grant_row photo.consent_grants%ROWTYPE;seen uuid[]:=ARRAY[]::uuid[];
 input_count integer;body_sha text;consent_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_workflow IS NULL OR p_task IS NULL OR
  p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 THEN
  RETURN false;
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT w.project_id,w.run_id INTO project,run_id
 FROM photo.v2_workflows w JOIN photo.runs r
  ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 WHERE w.tenant_id=p_tenant AND w.id=p_workflow AND t.id=p_task
  AND r.kind='v2' AND r.status='draft' AND NOT r.graph_sealed
  AND NOT r.cancel_requested AND t.operation='identity.compare'
  AND t.operation_version='1' AND t.resource_class='gpu'
  AND t.state='blocked' AND NOT t.cancel_requested
 FOR SHARE OF w,r,t;
 IF project IS NULL THEN RETURN false;END IF;
 body_sha:=encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex');
 SELECT count(*) INTO input_count FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.project_id=project AND i.task_id=p_task
  AND i.contract='ConsentSnapshot' AND i.sha256=body_sha;
 IF input_count<>1 OR (SELECT count(*) FROM photo.v2_task_inputs i
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task
    AND i.contract='ConsentSnapshot')<>1 THEN RETURN false;END IF;
 BEGIN body:=p_body::jsonb;
 EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value
  THEN RETURN false;
 END;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR
  body->>'schema_version' IS DISTINCT FROM '1' OR
  jsonb_typeof(body->'grants') IS DISTINCT FROM 'array' OR
  jsonb_typeof(body->'confirmed_faces') IS DISTINCT FROM 'array' OR
  jsonb_array_length(body->'grants')>5000 OR
  jsonb_array_length(body->'confirmed_faces')>12800 THEN RETURN false;END IF;
 FOR grant_item IN SELECT value FROM jsonb_array_elements(body->'grants') LOOP
  IF jsonb_typeof(grant_item) IS DISTINCT FROM 'object' OR
   grant_item->>'purpose' IS NULL OR
   grant_item->>'purpose' NOT IN
    ('identity_matching','learning_person','learning_photographer') OR
   grant_item->>'consent_id' IS NULL OR
   grant_item->>'epoch' IS NULL OR
   grant_item->>'consent_id' !~
    '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
   grant_item->>'epoch' !~ '^[1-9][0-9]{0,18}$' THEN RETURN false;END IF;
  IF grant_item->>'purpose'<>'identity_matching' THEN CONTINUE;END IF;
  IF grant_item->>'person_id' IS NULL OR grant_item->>'person_id' !~
   '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   THEN RETURN false;END IF;
  consent_id:=(grant_item->>'consent_id')::uuid;
  IF consent_id=ANY(seen) THEN RETURN false;END IF;
  seen:=array_append(seen,consent_id);
  SELECT * INTO grant_row FROM photo.consent_grants g
  WHERE g.tenant_id=p_tenant AND g.project_id=project AND g.id=consent_id
  FOR SHARE;
  IF grant_row.id IS NULL OR grant_row.status<>'granted' OR
   grant_row.epoch::text IS DISTINCT FROM grant_item->>'epoch' OR
   grant_row.purpose<>'identity_matching' OR
   grant_row.person_id::text IS DISTINCT FROM grant_item->>'person_id' OR
   NOT EXISTS(SELECT 1 FROM photo.project_persons person
    WHERE person.tenant_id=p_tenant AND person.project_id=project
     AND person.id=grant_row.person_id AND person.state='active')
   THEN RETURN false;END IF;
 END LOOP;
 FOREACH consent_id IN ARRAY seen LOOP
  SELECT * INTO grant_row FROM photo.consent_grants g
  WHERE g.tenant_id=p_tenant AND g.project_id=project AND g.id=consent_id
  FOR SHARE;
  INSERT INTO photo.workflow_consent_links(tenant_id,project_id,workflow_id,
   consent_id,consent_epoch)
  VALUES(p_tenant,project,p_workflow,consent_id,grant_row.epoch)
  ON CONFLICT (tenant_id,workflow_id,consent_id) DO NOTHING;
  IF NOT EXISTS(SELECT 1 FROM photo.workflow_consent_links link
   WHERE link.tenant_id=p_tenant AND link.project_id=project
    AND link.workflow_id=p_workflow AND link.consent_id=consent_id
    AND link.consent_epoch=grant_row.epoch) THEN
   RAISE EXCEPTION 'V23_IDENTITY_CONSENT_PIN_CONFLICT' USING ERRCODE='23514';
  END IF;
 END LOOP;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pin_v23_identity_compare_consents(uuid,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pin_v23_identity_compare_consents(uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.pin_v23_identity_compare_consents(uuid,uuid,uuid,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
