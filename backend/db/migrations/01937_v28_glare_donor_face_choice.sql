-- +goose Up
-- Resolve face IDs from confirmed observations in the same draft run. The
-- existing donor proof builder supplies current person, consent, session,
-- source-version and evidence locks before any ID is returned.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_glare_donor_face_choice(p_tenant uuid,p_project uuid,
 p_shoot uuid,p_run uuid,p_actor uuid,p_auth_session uuid,p_glare jsonb,
 p_source_observation uuid,p_donor_observation uuid) RETURNS jsonb
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;source_face uuid;donor_face uuid;
 open_params jsonb;proof jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_project IS NULL OR p_shoot IS NULL OR p_run IS NULL
 OR p_actor IS NULL OR p_auth_session IS NULL
 OR p_source_observation IS NULL OR p_donor_observation IS NULL
 OR p_source_observation=p_donor_observation
 OR jsonb_typeof(p_glare) IS DISTINCT FROM 'object'
 OR p_glare->>'mode' IS DISTINCT FROM 'donor'
 OR p_glare->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(p_glare->'donor') IS DISTINCT FROM 'object'
 THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
  AND kind='v2' AND status='draft' AND NOT graph_sealed
  AND NOT cancel_requested AND created_by=p_actor FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
  AND w.run_id=p_run AND w.project_id=p_project
  AND w.input_snapshot_id=r.input_snapshot_id AND w.workflow_type='preview'
  AND w.renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT source.face_id,donor.face_id INTO source_face,donor_face
 FROM photo.person_observations source JOIN photo.person_observations donor
  ON donor.tenant_id=source.tenant_id AND donor.project_id=source.project_id
  AND donor.person_id=source.person_id
 WHERE source.tenant_id=p_tenant AND source.project_id=p_project
  AND source.id=p_source_observation AND donor.id=p_donor_observation
  AND source.confirmed_by IS NOT NULL AND donor.confirmed_by IS NOT NULL
  AND source.asset_version_id=(p_glare->'base'->>'asset_version_id')::uuid
  AND donor.asset_version_id=(p_glare->'donor'->>'asset_version_id')::uuid
 FOR SHARE OF source,donor;
 IF NOT FOUND OR source_face IS NULL OR donor_face IS NULL
 THEN RETURN NULL;END IF;
 open_params:=jsonb_build_object('schema_version',1,'base',p_glare->'base',
  'donor',p_glare->'donor','strength',p_glare->'strength',
  'eyes_mask',p_glare->'glasses_mask','protected',p_glare->'protected',
  'confirmation',p_glare->'confirmation',
  'source_faces',p_glare->'source_faces','donor_faces',p_glare->'donor_faces',
  'source_face_id',source_face,'donor_face_id',donor_face);
 proof:=photo.build_v28_donor_confirmation(p_tenant,p_project,p_shoot,p_actor,
  p_auth_session,open_params,p_source_observation,p_donor_observation);
 IF proof IS NULL OR proof->>'source_face_id' IS DISTINCT FROM source_face::text
 OR proof->>'donor_face_id' IS DISTINCT FROM donor_face::text
 THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('source_face_id',source_face,
  'donor_face_id',donor_face);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_glare_donor_face_choice(uuid,uuid,uuid,uuid,uuid,uuid,
 jsonb,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_glare_donor_face_choice(uuid,uuid,uuid,uuid,
 uuid,uuid,jsonb,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_glare_donor_face_choice(uuid,uuid,uuid,uuid,
 uuid,uuid,jsonb,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
