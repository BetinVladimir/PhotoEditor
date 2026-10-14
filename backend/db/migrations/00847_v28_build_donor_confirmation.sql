-- +goose Up
-- Server derives person, consent epoch and actor; clients select observations only.
-- +goose StatementBegin
CREATE FUNCTION photo.build_v28_donor_confirmation(p_tenant uuid,p_project uuid,p_shoot uuid,
 p_actor uuid,p_auth_session uuid,p_params jsonb,p_source_observation uuid,p_donor_observation uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE person_id uuid;grant_id uuid;epoch_value text;proof jsonb;grant_count bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_source_observation IS NULL OR p_donor_observation IS NULL OR p_source_observation=p_donor_observation
  OR jsonb_typeof(p_params) IS DISTINCT FROM 'object'
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT source.person_id INTO person_id FROM photo.person_observations source
 JOIN photo.person_observations donor ON donor.tenant_id=source.tenant_id AND donor.project_id=source.project_id
  AND donor.person_id=source.person_id AND donor.id=p_donor_observation AND donor.confirmed_by IS NOT NULL
 WHERE source.tenant_id=p_tenant AND source.project_id=p_project AND source.id=p_source_observation
  AND source.confirmed_by IS NOT NULL
  AND source.asset_version_id=(p_params->'base'->>'asset_version_id')::uuid
  AND donor.asset_version_id=(p_params->'donor'->>'asset_version_id')::uuid
  AND source.face_id=(p_params->>'source_face_id')::uuid AND donor.face_id=(p_params->>'donor_face_id')::uuid;
 IF NOT FOUND THEN RETURN NULL;END IF;
 -- Ambiguous current grants must be resolved explicitly, never selected by LIMIT.
 SELECT count(*),min(g.id::text)::uuid,min(g.epoch)::text INTO grant_count,grant_id,epoch_value
 FROM photo.consent_grants g JOIN photo.project_persons person ON person.tenant_id=g.tenant_id
  AND person.project_id=g.project_id AND person.id=g.person_id AND person.state='active' AND person.consent_epoch=g.epoch
 WHERE g.tenant_id=p_tenant AND g.project_id=p_project AND g.person_id=person_id
  AND g.purpose='identity_matching' AND g.status='granted';
 IF grant_count<>1 THEN RETURN NULL;END IF;
 proof:=jsonb_build_object('schema_version',1,'person_id',person_id,'source',p_params->'base','donor',p_params->'donor',
  'source_face_id',p_params->>'source_face_id','donor_face_id',p_params->>'donor_face_id',
  'source_observation_id',p_source_observation,'donor_observation_id',p_donor_observation,
  'consent_id',grant_id,'epoch',epoch_value,'actor_id',p_actor);
 IF photo.lock_v28_donor_session(p_tenant,p_project,p_shoot,p_actor,p_auth_session,proof,
  p_params->'source_faces',p_params->'donor_faces') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN proof;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.build_v28_donor_confirmation(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.build_v28_donor_confirmation(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.build_v28_donor_confirmation(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
