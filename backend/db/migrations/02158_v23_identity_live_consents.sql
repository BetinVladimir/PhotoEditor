-- +goose Up
-- Called inside the future identity adoption transaction. Tenant row locking
-- serializes this check with consent revoke; no independent read is a receipt.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_identity_live_consents(p_tenant uuid,p_task uuid,
 p_result uuid,p_snapshot uuid,p_snapshot_sha text,p_body text,
 p_before uuid,p_after uuid) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid;workflow uuid;task_row photo.tasks%ROWTYPE;
 body jsonb;grant_item jsonb;face_item jsonb;grant_row photo.consent_grants%ROWTYPE;
 active_ids uuid[]:=ARRAY[]::uuid[];consent_id uuid;match_count integer:=0;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM
  nullif(current_setting('app.tenant_id',true),'')::uuid OR
  p_task IS NULL OR p_result IS NULL OR p_snapshot IS NULL OR
  p_before IS NULL OR p_after IS NULL OR p_before=p_after OR
  p_snapshot_sha !~ '^[0-9a-f]{64}$' OR p_body IS NULL OR
  octet_length(p_body) NOT BETWEEN 2 AND 8388608 OR
  encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_snapshot_sha OR
  photo.v23_identity_snapshot_relations_ready(p_body) IS DISTINCT FROM true
  THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
  AND operation='identity.compare' AND operation_version='1' AND
  state='succeeded' AND NOT cancel_requested AND
  canonical_result_id=p_result AND attempt>0 AND fencing_token>0 FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT w.id,w.project_id INTO workflow,project FROM photo.v2_workflows w
  JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  WHERE w.tenant_id=p_tenant AND w.run_id=task_row.run_id AND
   p.archived_at IS NULL AND NOT r.cancel_requested;
 IF workflow IS NULL OR NOT EXISTS(SELECT 1 FROM photo.task_results z
  WHERE z.tenant_id=p_tenant AND z.id=p_result AND z.task_id=p_task AND
   z.attempt=task_row.attempt AND z.fencing_token=task_row.fencing_token) OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.project_id=project AND i.task_id=p_task AND
   i.contract='ConsentSnapshot')<>1 OR
  NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.project_id=project AND i.task_id=p_task AND
   i.contract='ConsentSnapshot' AND i.artifact_id=p_snapshot AND
   i.sha256=p_snapshot_sha) THEN RETURN false;END IF;
 body:=p_body::jsonb;
 FOR grant_item IN SELECT value FROM jsonb_array_elements(body->'grants') LOOP
  IF grant_item->>'purpose'<>'identity_matching' THEN CONTINUE;END IF;
  consent_id:=(grant_item->>'consent_id')::uuid;
  IF consent_id=ANY(active_ids) THEN RETURN false;END IF;
  active_ids:=array_append(active_ids,consent_id);
  SELECT * INTO grant_row FROM photo.consent_grants g WHERE
   g.tenant_id=p_tenant AND g.project_id=project AND g.id=consent_id
   FOR SHARE;
  IF grant_row.id IS NULL OR grant_row.status<>'granted' OR
   grant_row.epoch::text IS DISTINCT FROM grant_item->>'epoch' OR
   grant_row.purpose<>'identity_matching' OR
   grant_row.person_id::text IS DISTINCT FROM grant_item->>'person_id' OR
   NOT EXISTS(SELECT 1 FROM photo.project_persons person WHERE
    person.tenant_id=p_tenant AND person.project_id=project AND
    person.id=grant_row.person_id AND person.state='active') OR
   NOT EXISTS(SELECT 1 FROM photo.workflow_consent_links link WHERE
    link.tenant_id=p_tenant AND link.project_id=project AND
    link.workflow_id=workflow AND link.consent_id=consent_id AND
    link.consent_epoch=grant_row.epoch) THEN RETURN false;END IF;
  match_count:=match_count+1;
 END LOOP;
 IF (SELECT count(*) FROM photo.workflow_consent_links link WHERE
  link.tenant_id=p_tenant AND link.project_id=project AND
  link.workflow_id=workflow)<>match_count THEN RETURN false;END IF;
 FOR face_item IN SELECT value FROM jsonb_array_elements(body->'confirmed_faces') LOOP
  IF (face_item->>'asset_version_id')::uuid NOT IN(p_before,p_after) OR
   NOT EXISTS(SELECT 1 FROM photo.person_observations observation WHERE
    observation.tenant_id=p_tenant AND observation.project_id=project AND
    observation.asset_version_id=(face_item->>'asset_version_id')::uuid AND
    observation.face_id=(face_item->>'face_id')::uuid AND
    observation.person_id=(face_item->>'person_id')::uuid AND
    observation.confirmed_by IS NOT NULL) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.v23_identity_live_consents(uuid,uuid,uuid,uuid,text,text,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_identity_live_consents(uuid,uuid,uuid,uuid,text,text,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v23_identity_live_consents(uuid,uuid,uuid,uuid,text,text,uuid,uuid)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
