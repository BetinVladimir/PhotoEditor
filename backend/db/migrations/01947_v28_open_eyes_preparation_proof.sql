-- +goose Up
-- Read-only current donor consent/face proof before a run is created.
-- Params are server-compiled and the caller still must issue/publish a
-- confirmation and repeat all authority during draft/admission.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_open_eyes_preparation_proof(p_tenant uuid,
 p_project uuid,p_shoot uuid,p_snapshot uuid,p_actor uuid,p_session uuid,
 p_params jsonb,p_pin jsonb,p_source_observation uuid,p_donor_observation uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_face uuid;donor_face uuid;bound jsonb;proof jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_project IS NULL OR p_shoot IS NULL OR p_snapshot IS NULL
 OR p_actor IS NULL OR p_session IS NULL OR p_source_observation IS NULL
 OR p_donor_observation IS NULL OR p_source_observation=p_donor_observation
 OR jsonb_typeof(p_params) IS DISTINCT FROM 'object'
 OR p_params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(p_params->'base') IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_params->'donor') IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_pin) IS DISTINCT FROM 'object'
 THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.snapshots s WHERE s.tenant_id=p_tenant AND s.id=p_snapshot
  AND s.kind='v2' AND s.principal_type='user' AND s.principal_id=p_actor
  AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND OR photo.lock_v28_donor_qualification(p_tenant,p_project,p_actor,
  p_session,p_pin) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT source.face_id,donor.face_id INTO source_face,donor_face
 FROM photo.person_observations source JOIN photo.person_observations donor
  ON donor.tenant_id=source.tenant_id AND donor.project_id=source.project_id
  AND donor.person_id=source.person_id
 WHERE source.tenant_id=p_tenant AND source.project_id=p_project
  AND source.id=p_source_observation AND donor.id=p_donor_observation
  AND source.confirmed_by IS NOT NULL AND donor.confirmed_by IS NOT NULL
  AND source.asset_version_id=(p_params->'base'->>'asset_version_id')::uuid
  AND donor.asset_version_id=(p_params->'donor'->>'asset_version_id')::uuid
 FOR SHARE OF source,donor;
 IF NOT FOUND OR source_face IS NULL OR donor_face IS NULL THEN RETURN NULL;END IF;
 bound:=p_params||jsonb_build_object('source_face_id',source_face,
  'donor_face_id',donor_face);
 proof:=photo.build_v28_donor_confirmation(p_tenant,p_project,p_shoot,p_actor,
  p_session,bound,p_source_observation,p_donor_observation);
 IF proof IS NULL OR proof->>'source_face_id' IS DISTINCT FROM source_face::text
 OR proof->>'donor_face_id' IS DISTINCT FROM donor_face::text THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('proof',proof,'params',bound);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v28_open_eyes_preparation_proof(uuid,uuid,uuid,uuid,
 uuid,uuid,jsonb,jsonb,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_open_eyes_preparation_proof(uuid,uuid,uuid,
 uuid,uuid,jsonb,jsonb,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_open_eyes_preparation_proof(uuid,uuid,
 uuid,uuid,uuid,uuid,jsonb,jsonb,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
