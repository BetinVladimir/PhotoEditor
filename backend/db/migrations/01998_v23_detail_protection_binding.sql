-- Persist the exact training protection before the pending recovery manifest
-- is replaced by the admission ticket. Evaluate must close against this pin.
-- +goose Up
ALTER TABLE photo.backend_documents
 ADD COLUMN v23_detail_protection_id uuid,
 ADD COLUMN v23_detail_protection_sha text;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v23_detail_protection_pair
 CHECK ((v23_detail_protection_id IS NULL AND v23_detail_protection_sha IS NULL)
  OR (purpose='v23_detail_holdout' AND v23_detail_protection_id IS NOT NULL
   AND v23_detail_protection_sha ~ '^[0-9a-f]{64}$'));

ALTER FUNCTION photo.reserve_v23_detail_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text)
 RENAME TO reserve_v23_detail_document_pre_protection;
REVOKE ALL ON FUNCTION photo.reserve_v23_detail_document_pre_protection(
 uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text) FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v23_detail_document(
 p_tenant uuid,p_run uuid,p_event uuid,p_project uuid,p_actor uuid,
 p_auth_session uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE reservation record; recovery jsonb; example jsonb; detail jsonb;
 detail_entry jsonb; body_text text;
 protection jsonb; d photo.backend_documents%ROWTYPE;
BEGIN
 SELECT * INTO reservation FROM photo.reserve_v23_detail_document_pre_protection(
  p_tenant,p_run,p_event,p_project,p_actor,p_auth_session,p_sha,p_bytes,p_bucket);
 IF NOT FOUND THEN RETURN; END IF;
 IF reservation.outcome NOT IN ('created','resumed','replayed') THEN
  RETURN QUERY SELECT reservation.outcome,reservation.artifact_id,
   reservation.token,reservation.ready; RETURN;
 END IF;
 SELECT i.response_body INTO recovery FROM photo.idempotency_records i
  WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND
   i.principal_id=p_actor AND i.route='V2.Admit' AND
   i.resource_id=p_run AND i.response_status IS NULL;
 IF recovery IS NULL OR
  (SELECT count(*) FROM photo.idempotency_records i WHERE
   i.tenant_id=p_tenant AND i.principal_type='user' AND
   i.principal_id=p_actor AND i.route='V2.Admit' AND
   i.resource_id=p_run AND i.response_status IS NULL)<>1 OR
  jsonb_typeof(recovery->'manifest_jcs')<>'string' OR
  jsonb_typeof(recovery->'detail_jcs')<>'string' OR
  (SELECT count(*) FROM jsonb_array_elements(
   (recovery->>'manifest_jcs')::jsonb->'examples') x
   WHERE x.value->>'event_id'=p_event::text)<>1 OR
  (SELECT count(*) FROM jsonb_array_elements(
   (recovery->>'detail_jcs')::jsonb) x
   WHERE x.value->>'event_id'=p_event::text)<>1 THEN
  RAISE EXCEPTION 'V23_DETAIL_PROTECTION_RECOVERY_MISSING' USING ERRCODE='23514';
 END IF;
 SELECT x.value INTO example FROM jsonb_array_elements(
  (recovery->>'manifest_jcs')::jsonb->'examples') x
  WHERE x.value->>'event_id'=p_event::text;
 SELECT x.value INTO detail_entry FROM jsonb_array_elements(
  (recovery->>'detail_jcs')::jsonb) x
  WHERE x.value->>'event_id'=p_event::text;
 body_text:=detail_entry->>'body_jcs';
 detail:=body_text::jsonb;
 protection:=example->'render_case'->'protected';
 IF jsonb_typeof(protection)<>'object' OR
  (protection-'artifact_id'-'sha256')<>'{}'::jsonb OR
  coalesce(protection->>'artifact_id','') !~
   '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
  coalesce(protection->>'sha256','') !~ '^[0-9a-f]{64}$' OR
  detail->'protection' IS DISTINCT FROM protection OR
  encode(public.digest(convert_to(body_text,'UTF8'),'sha256'),'hex')
   IS DISTINCT FROM p_sha THEN
  RAISE EXCEPTION 'V23_DETAIL_PROTECTION_RECOVERY_CHANGED' USING ERRCODE='23514';
 END IF;
 SELECT * INTO d FROM photo.backend_documents x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.owner_id=p_event AND
  x.purpose='v23_detail_holdout' FOR UPDATE;
 IF d.id IS NULL OR d.artifact_id IS DISTINCT FROM reservation.artifact_id OR
  (d.v23_detail_protection_id IS NOT NULL AND
   d.v23_detail_protection_id IS DISTINCT FROM
    (protection->>'artifact_id')::uuid) OR
  (d.v23_detail_protection_sha IS NOT NULL AND
   d.v23_detail_protection_sha IS DISTINCT FROM protection->>'sha256') THEN
  RAISE EXCEPTION 'V23_DETAIL_PROTECTION_BINDING_CHANGED' USING ERRCODE='23514';
 END IF;
 UPDATE photo.backend_documents SET
  v23_detail_protection_id=(protection->>'artifact_id')::uuid,
  v23_detail_protection_sha=protection->>'sha256'
  WHERE id=d.id AND v23_detail_protection_id IS NULL;
 RETURN QUERY SELECT reservation.outcome,reservation.artifact_id,
  reservation.token,reservation.ready;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v23_detail_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v23_detail_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v23_detail_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text) TO photo_scheduler;

ALTER FUNCTION photo.complete_v23_detail_document(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)
 RENAME TO complete_v23_detail_document_pre_protection;
REVOKE ALL ON FUNCTION photo.complete_v23_detail_document_pre_protection(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)
 FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v23_detail_document(
 p_tenant uuid,p_run uuid,p_event uuid,p_project uuid,p_actor uuid,
 p_auth_session uuid,p_artifact uuid,p_token uuid,p_sha text,
 p_bytes bigint,p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE completed boolean; d photo.backend_documents%ROWTYPE;
BEGIN
 completed:=photo.complete_v23_detail_document_pre_protection(
  p_tenant,p_run,p_event,p_project,p_actor,p_auth_session,p_artifact,
  p_token,p_sha,p_bytes,p_bucket,p_version);
 IF completed IS DISTINCT FROM true THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.backend_documents x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.owner_id=p_event AND
  x.purpose='v23_detail_holdout' AND x.artifact_id=p_artifact AND
  x.sha256=p_sha AND x.state='ready' FOR SHARE;
 IF d.id IS NULL OR d.v23_detail_protection_id IS NULL OR
  d.v23_detail_protection_sha IS NULL THEN
  -- The core may have inserted an artifact and charged quota. An exception
  -- rolls those writes back with the document transition.
  RAISE EXCEPTION 'V23_DETAIL_PROTECTION_NOT_BOUND' USING ERRCODE='23514';
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v23_detail_document(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v23_detail_document(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v23_detail_document(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_detail_protection_current(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item record; a photo.artifacts%ROWTYPE; total bigint; found_count bigint:=0;
BEGIN
 SELECT count(*) INTO total FROM photo.v2_task_inputs i WHERE
  i.tenant_id=p_tenant AND i.task_id=p_task AND
  i.contract='FrozenPortraitDetailHoldoutEvidence';
 FOR item IN SELECT d.v23_detail_protection_id AS id,
   d.v23_detail_protection_sha AS sha, i.artifact_id AS evidence_id
  FROM photo.v2_task_inputs i JOIN photo.backend_documents d ON
   d.tenant_id=i.tenant_id AND d.artifact_id=i.artifact_id AND
   d.sha256=i.sha256 AND d.run_id=p_run AND
   d.purpose='v23_detail_holdout' AND d.state='ready'
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.contract='FrozenPortraitDetailHoldoutEvidence'
  ORDER BY i.artifact_id FOR SHARE OF d
 LOOP
  found_count:=found_count+1;
  IF item.id IS NULL OR item.sha IS NULL OR
   NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs x WHERE
    x.tenant_id=p_tenant AND x.task_id=p_task AND
    x.artifact_id=item.id AND x.sha256=item.sha AND x.contract='MaskRef') OR
   NOT EXISTS(SELECT 1 FROM photo.graph_expansions e JOIN photo.tasks fit
    ON fit.tenant_id=e.tenant_id AND fit.run_id=e.run_id AND
    fit.id=e.parent_task_id AND fit.operation='learning.fit' AND
    fit.operation_version='1' AND fit.state='succeeded' AND
    fit.canonical_result_id IS NOT NULL JOIN photo.v2_task_inputs fi ON
    fi.tenant_id=fit.tenant_id AND fi.task_id=fit.id AND
    fi.artifact_id=item.id AND fi.sha256=item.sha AND
    fi.contract='MaskRef' WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND
    e.id=p_expansion AND e.expansion_token=p_token AND
    e.expansion_lease_until>clock_timestamp() AND
    e.kind='v2.learning.evaluate/1' AND e.state='open' AND
    EXISTS(SELECT 1 FROM photo.v2_task_inputs ei WHERE
     ei.tenant_id=fit.tenant_id AND ei.task_id=fit.id AND
     ei.artifact_id=item.evidence_id AND
     ei.contract='FrozenPortraitDetailHoldoutEvidence')) THEN RETURN false; END IF;
  SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND
   x.id=item.id AND x.sha256=item.sha AND x.state='ready' AND
   x.verified_at IS NOT NULL AND x.media_type='application/json' AND
   x.object_version IS NOT NULL AND x.object_version NOT IN ('','null') AND
   (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) FOR SHARE;
  IF NOT FOUND THEN RETURN false; END IF;
 END LOOP;
 RETURN found_count=total;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_detail_protection_current(uuid,uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_detail_protection_current(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.close_v23_learning_evaluate_guarded(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.v23_evaluate_defect_closure_current(p_tenant,p_run,
  p_expansion,p_token,p_task) OR
  NOT photo.v23_evaluate_dust_closure_current(p_tenant,p_run,
   p_expansion,p_token,p_task) OR
  NOT photo.v23_evaluate_detail_closure_current(p_tenant,p_run,
   p_expansion,p_token,p_task) OR
  NOT photo.v23_evaluate_detail_protection_current(p_tenant,p_run,
   p_expansion,p_token,p_task)
  THEN RETURN false; END IF;
 RETURN photo.close_v23_learning_evaluate(p_tenant,p_run,
  p_expansion,p_token,p_task);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.close_v23_learning_evaluate_guarded(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.close_v23_learning_evaluate_guarded(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.close_v23_learning_evaluate_guarded(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
