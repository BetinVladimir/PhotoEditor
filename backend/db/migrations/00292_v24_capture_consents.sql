-- +goose Up
ALTER TABLE photo.capture_assessment_jobs ADD COLUMN consent_seed jsonb
 CHECK(consent_seed IS NULL OR (jsonb_typeof(consent_seed)='object' AND octet_length(consent_seed::text)<=8388608));
ALTER TABLE photo.capture_assessment_jobs ADD COLUMN consent_people uuid[];
ALTER TABLE photo.capture_assessment_jobs ADD CONSTRAINT capture_consent_seed_pair
 CHECK((consent_seed IS NULL)=(consent_people IS NULL));
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.guard_capture_selection_seed() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.selection_seed IS NOT NULL AND NEW.selection_seed IS DISTINCT FROM OLD.selection_seed THEN
  RAISE EXCEPTION 'immutable capture selection' USING ERRCODE='23514';
 END IF;
 IF OLD.consent_seed IS NOT NULL AND (NEW.consent_seed IS DISTINCT FROM OLD.consent_seed
 OR NEW.consent_people IS DISTINCT FROM OLD.consent_people) THEN
  RAISE EXCEPTION 'immutable capture consent seed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_capture_selection_seed() OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.freeze_capture_consents(p_tenant uuid,p_job uuid,p_token uuid,p_required uuid[])
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE job photo.capture_assessment_jobs%ROWTYPE; required uuid[]; people uuid[];
 person uuid; result jsonb; session_epoch bigint; grant_row jsonb;
BEGIN
 IF p_required IS NULL OR cardinality(p_required)>5000 OR array_position(p_required,NULL) IS NOT NULL
 OR '00000000-0000-0000-0000-000000000000'::uuid=ANY(p_required) THEN
  RAISE EXCEPTION 'invalid capture consent people' USING ERRCODE='22023';
 END IF;
 SELECT coalesce(array_agg(DISTINCT id ORDER BY id),'{}'::uuid[]) INTO required FROM unnest(p_required) id;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO job FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR job.state<>'leased' OR job.lease_token IS DISTINCT FROM p_token
 OR job.lease_until<=clock_timestamp() THEN RETURN NULL; END IF;
 IF job.selection_seed IS NULL THEN RAISE EXCEPTION 'capture selection missing' USING ERRCODE='23514'; END IF;
 SELECT consent_epoch INTO session_epoch FROM photo.shoot_sessions WHERE tenant_id=p_tenant AND project_id=job.project_id AND id=job.session_id FOR SHARE;
 IF session_epoch IS DISTINCT FROM (job.selection_seed->>'consent_epoch')::bigint THEN
  RAISE EXCEPTION 'V24_CAPTURE_CONTEXT_STALE' USING ERRCODE='23514';
 END IF;
 IF job.consent_seed IS NOT NULL THEN
  IF job.consent_people IS DISTINCT FROM required THEN RAISE EXCEPTION 'capture consent replay conflict' USING ERRCODE='23514'; END IF;
  FOR grant_row IN SELECT value FROM jsonb_array_elements(job.consent_seed->'grants') ORDER BY value->>'person_id' LOOP
   PERFORM id FROM photo.project_persons WHERE tenant_id=p_tenant AND project_id=job.project_id
    AND id=(grant_row->>'person_id')::uuid AND state='active' FOR SHARE;
   IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
   PERFORM id FROM photo.consent_grants WHERE tenant_id=p_tenant AND project_id=job.project_id
    AND id=(grant_row->>'consent_id')::uuid AND person_id=(grant_row->>'person_id')::uuid
    AND purpose='identity_matching' AND status='granted' AND epoch=(grant_row->>'epoch')::bigint FOR SHARE;
   IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
  END LOOP;
  RETURN job.consent_seed;
 END IF;
 SELECT coalesce(array_agg(DISTINCT id ORDER BY id),'{}'::uuid[]) INTO people FROM (
  SELECT unnest(required) id UNION SELECT observation.person_id FROM photo.person_observations observation
  WHERE observation.tenant_id=p_tenant AND observation.project_id=job.project_id
  AND observation.asset_version_id=job.asset_version_id AND observation.confirmed_by IS NOT NULL
 ) ids;
 IF cardinality(people)>5000 THEN RAISE EXCEPTION 'too many capture consent people' USING ERRCODE='22023'; END IF;
 FOREACH person IN ARRAY people LOOP
  PERFORM id FROM photo.project_persons WHERE tenant_id=p_tenant AND project_id=job.project_id AND id=person AND state='active' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
  PERFORM id FROM photo.consent_grants WHERE tenant_id=p_tenant AND project_id=job.project_id AND person_id=person
   AND purpose='identity_matching' AND status='granted' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
 END LOOP;
 WITH grants AS (
  SELECT id,person_id,epoch FROM photo.consent_grants WHERE tenant_id=p_tenant AND project_id=job.project_id
  AND person_id=ANY(people) AND purpose='identity_matching' AND status='granted'
 ), faces AS (
  SELECT o.asset_version_id,o.face_id,o.person_id,g.id consent_id,g.epoch FROM photo.person_observations o
  JOIN grants g ON g.person_id=o.person_id WHERE o.tenant_id=p_tenant AND o.project_id=job.project_id
  AND o.asset_version_id=job.asset_version_id AND o.confirmed_by IS NOT NULL
 )
 SELECT jsonb_build_object('schema_version',1,
  'grants',(SELECT coalesce(jsonb_agg(jsonb_build_object('consent_id',id,'person_id',person_id,'epoch',epoch::text,'purpose','identity_matching') ORDER BY id),'[]'::jsonb) FROM grants),
  'confirmed_faces',(SELECT coalesce(jsonb_agg(jsonb_build_object('asset_version_id',asset_version_id,'face_id',face_id,'person_id',person_id,'consent_id',consent_id,'epoch',epoch::text) ORDER BY face_id),'[]'::jsonb) FROM faces)) INTO result;
 IF jsonb_array_length(result->'confirmed_faces')>12800 THEN RAISE EXCEPTION 'too many capture faces' USING ERRCODE='22023'; END IF;
 UPDATE photo.capture_assessment_jobs SET consent_seed=result,consent_people=required WHERE tenant_id=p_tenant AND id=p_job;
 RETURN result;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.freeze_capture_consents(uuid,uuid,uuid,uuid[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.freeze_capture_consents(uuid,uuid,uuid,uuid[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.freeze_capture_consents(uuid,uuid,uuid,uuid[]) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
