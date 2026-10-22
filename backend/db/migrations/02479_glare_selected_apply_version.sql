-- +goose Up
-- Human-selected glare persistence uses current restoration authority, never
-- a fabricated running-worker lease or the open-eyes donor qualification.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_glare_selected_authority(p_tenant uuid,p_review uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;plan jsonb;body jsonb;pin jsonb;d photo.backend_documents%ROWTYPE;c jsonb;proof text;selected record;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL THEN RETURN false;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review AND kind='portrait.glare'
  AND state='decided' AND revision=2 FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=v.run_id AND kind='v2'
  AND status IN('waiting_review','completed') AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=v.task_id
  AND operation='portrait.glare' AND operation_version='1' AND state='succeeded' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=t.canonical_result_id
  AND attempt=t.attempt AND fencing_token=t.fencing_token AND review_required FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND workflow_type='preview'
  AND renderer_version='v2.2' AND input_snapshot_id=r.input_snapshot_id FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO selected FROM photo.get_v28_restoration_review_selection(p_tenant,p_actor,p_session,p_review);
 IF NOT FOUND OR selected.route NOT IN('candidate','base') THEN RETURN false;END IF;
 plan:=r.v28_restoration_descriptor_jcs::jsonb;body:=plan->'params';pin:=t.execution_profile->'restoration_qualification';
 IF body->>'mode' NOT IN('donor','generative') OR body->>'schema_version' IS DISTINCT FROM '1'
 OR plan->'task'->>'id' IS DISTINCT FROM t.id::text
 OR pin IS DISTINCT FROM plan->'task'->'execution_profile'->'restoration_qualification'
 OR pin->>'backend' IS DISTINCT FROM t.execution_profile->>'backend'
 OR pin->>'model_lock_sha' IS DISTINCT FROM body->>'model_lock_sha'
 OR pin->>'runtime_lock_sha' IS DISTINCT FROM t.execution_profile->>'restoration_runtime_sha'
 OR pin->'calibration' IS DISTINCT FROM body->'calibration'
 OR body->'base'->'source'->>'artifact_id' IS DISTINCT FROM v.base_artifact_id::text
 OR NOT EXISTS(SELECT FROM photo.snapshot_items si JOIN photo.asset_versions version ON version.tenant_id=si.tenant_id AND version.id=si.asset_version_id
  JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
  JOIN photo.artifacts image ON image.tenant_id=version.tenant_id AND image.id=version.master_artifact_id AND image.sha256=version.content_hash
  WHERE si.tenant_id=p_tenant AND si.snapshot_id=r.input_snapshot_id
   AND si.asset_version_id=(body->'base'->>'asset_version_id')::uuid
   AND si.artifact_id=v.base_artifact_id AND si.sha256=body->'base'->'source'->>'sha256'
   AND version.master_artifact_id=si.artifact_id AND version.content_hash=si.sha256 AND version.review_state='accepted'
   AND asset.project_id=w.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
   AND image.state='ready' AND image.verified_at IS NOT NULL AND image.object_version NOT IN('','null')
   AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp()))
 OR photo.lock_v28_restoration_qualification(p_tenant,w.project_id,p_actor,p_session,'portrait.glare',pin) IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM artifact.id FROM photo.artifacts artifact WHERE artifact.tenant_id=p_tenant
  AND artifact.id IN(v.base_artifact_id,v.candidate_artifact_id) ORDER BY artifact.id FOR SHARE;
 SELECT doc.* INTO d FROM photo.backend_documents doc JOIN photo.artifacts artifact
  ON artifact.tenant_id=doc.tenant_id AND artifact.id=doc.artifact_id AND artifact.sha256=doc.sha256
  WHERE doc.tenant_id=p_tenant AND doc.run_id=r.id AND doc.owner_id=t.id AND doc.purpose='v28_donor_confirmation'
   AND doc.state='ready' AND doc.artifact_id=(body->'confirmation'->>'artifact_id')::uuid
   AND doc.sha256=body->'confirmation'->>'sha256' AND artifact.output_role='v28_donor_confirmation'
   AND artifact.state='ready' AND artifact.verified_at IS NOT NULL AND artifact.bytes=doc.bytes AND artifact.bucket=doc.bucket
   AND artifact.object_version=doc.object_version AND artifact.object_version NOT IN('','null')
   AND artifact.object_key='tenants/'||p_tenant||'/runs/'||r.id||'/documents/'||doc.artifact_id
   AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp()) FOR SHARE OF doc,artifact;
 IF NOT FOUND THEN RETURN false;END IF;
 c:=d.v28_donor_issuer_context;proof:=c->>'proof_jcs';
 IF proof IS NULL OR octet_length(proof) NOT BETWEEN 2 AND 65536
 OR d.bytes IS DISTINCT FROM octet_length(proof)::bigint
 OR encode(sha256(convert_to(proof,'UTF8')),'hex') IS DISTINCT FROM d.sha256
 OR c->>'tenant_id' IS DISTINCT FROM p_tenant::text OR c->>'run_id' IS DISTINCT FROM r.id::text
 OR c->>'project_id' IS DISTINCT FROM w.project_id::text OR c->>'actor_id' IS DISTINCT FROM r.created_by::text
 OR c->'params'->'base' IS DISTINCT FROM body->'base' OR c->'params'->'donor' IS DISTINCT FROM body->'donor'
 OR c->'params'->'source_faces' IS DISTINCT FROM body->'source_faces'
 OR c->'params'->'donor_faces' IS DISTINCT FROM body->'donor_faces'
 OR c->'params'->'eyes_mask' IS DISTINCT FROM body->'glasses_mask'
 OR c->'params'->'protected' IS DISTINCT FROM body->'protected'
 OR c->'params'->'strength' IS DISTINCT FROM body->'strength'
 OR c->'params'->'confirmation'->>'artifact_id' IS DISTINCT FROM d.owner_id::text
 OR c->'params'->'confirmation'->>'sha256' IS DISTINCT FROM d.sha256
 OR proof::jsonb->'source' IS DISTINCT FROM body->'base'
 OR proof::jsonb->'donor' IS DISTINCT FROM body->'donor' THEN RETURN false;END IF;
 IF body->>'mode'='generative' THEN
  IF coalesce(body->'donor','null'::jsonb)<>'null'::jsonb OR coalesce(body->'donor_faces','null'::jsonb)<>'null'::jsonb
   OR proof::jsonb->>'actor_id' IS DISTINCT FROM p_actor::text
   OR coalesce(proof::jsonb->'donor','null'::jsonb)<>'null'::jsonb
   OR coalesce(proof::jsonb->'donor_face_id','null'::jsonb)<>'null'::jsonb
   OR coalesce(proof::jsonb->'donor_observation_id','null'::jsonb)<>'null'::jsonb
   OR r.v28_restoration_adoption_receipt#>>'{evidence,operation}' IS DISTINCT FROM 'portrait.glare'
   OR jsonb_array_length(r.v28_restoration_adoption_receipt#>'{evidence,objects}') IS DISTINCT FROM 12 THEN RETURN false;END IF;
  PERFORM person.id FROM photo.project_persons person WHERE person.tenant_id=p_tenant AND person.project_id=w.project_id
   AND person.id=(proof::jsonb->>'person_id')::uuid AND person.state='active'
   AND person.consent_epoch::text=proof::jsonb->>'epoch' FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
  PERFORM grant_row.id FROM photo.consent_grants grant_row WHERE grant_row.tenant_id=p_tenant AND grant_row.project_id=w.project_id
   AND grant_row.id=(proof::jsonb->>'consent_id')::uuid AND grant_row.person_id=(proof::jsonb->>'person_id')::uuid
   AND grant_row.purpose='identity_matching' AND grant_row.status='granted' AND grant_row.epoch::text=proof::jsonb->>'epoch' FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
  PERFORM observation.id FROM photo.person_observations observation JOIN photo.artifacts evidence
   ON evidence.tenant_id=observation.tenant_id AND evidence.id=observation.evidence_artifact_id
   WHERE observation.tenant_id=p_tenant AND observation.project_id=w.project_id
    AND observation.id=(proof::jsonb->>'source_observation_id')::uuid
    AND observation.person_id=(proof::jsonb->>'person_id')::uuid AND observation.confirmed_by IS NOT NULL
    AND observation.asset_version_id=(body->'base'->>'asset_version_id')::uuid
    AND observation.face_id=(proof::jsonb->>'source_face_id')::uuid
    AND evidence.id=(body->'source_faces'->>'artifact_id')::uuid AND evidence.sha256=body->'source_faces'->>'sha256'
    AND evidence.state='ready' AND evidence.verified_at IS NOT NULL AND evidence.object_version NOT IN('','null')
    AND (evidence.expires_at IS NULL OR evidence.expires_at>clock_timestamp()) FOR SHARE OF observation,evidence;
  RETURN FOUND;
 END IF;
 RETURN photo.lock_v28_donor_session(p_tenant,w.project_id,(c->>'shoot_session_id')::uuid,p_actor,p_session,proof::jsonb,body->'source_faces',body->'donor_faces');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_glare_selected_authority(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_glare_selected_authority(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
-- +goose StatementEnd
-- The already shared same-canvas WorkingImage persistence invariant is reused,
-- but every glare authority and descriptor is explicitly replaced below. No
-- worker lease or open-eyes qualification function is copied.
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;replacement text;BEGIN
 definition:=pg_get_functiondef('photo.apply_v28_open_eyes_version(uuid,uuid,uuid,uuid,text,text,text)'::regprocedure);
 definition:=replace(definition,'photo.apply_v28_open_eyes_version(','photo.apply_v28_glare_version(');
 definition:=replace(definition,'''portrait.open_eyes''','''portrait.glare''');
 definition:=replace(definition,'v28_donor_descriptor_jcs','v28_restoration_descriptor_jcs');
 definition:=replace(definition,'photo.get_v28_open_eyes_review_selection(','photo.get_v28_restoration_review_selection(');
 definition:=replace(definition,'photo.v28_open_eyes_selected_authority(','photo.v28_glare_selected_authority(');
 definition:=replace(definition,'V28.OpenEyesAppliedVersion','V28.GlareAppliedVersion');
 definition:=replace(definition,'''open_eyes_source''','''glare_source''');
 definition:=replace(definition,'''open_eyes_donor''','''glare_donor''');
 needle:='AND EXISTS(SELECT FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity AND source_version_id=(body->''donor''->>''asset_version_id'')::uuid AND role=''glare_donor'' AND ordinal=1)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'GLARE_IDEMPOTENT_DONOR_ANCHOR';END IF;
 replacement:='AND ((body->>''mode''=''donor'' AND EXISTS(SELECT FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity AND source_version_id=(body->''donor''->>''asset_version_id'')::uuid AND role=''glare_donor'' AND ordinal=1)) OR (body->>''mode''=''generative'' AND NOT EXISTS(SELECT FROM photo.asset_version_sources WHERE tenant_id=p_tenant AND version_id=identity AND role=''glare_donor'')))';
 definition:=replace(definition,needle,replacement);
 needle:='VALUES(p_tenant,identity,src.id,''glare_source'',0),(p_tenant,identity,(body->''donor''->>''asset_version_id'')::uuid,''glare_donor'',1);';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'GLARE_SOURCE_LINEAGE_ANCHOR';END IF;
 replacement:='VALUES(p_tenant,identity,src.id,''glare_source'',0); IF body->>''mode''=''donor'' THEN INSERT INTO photo.asset_version_sources(tenant_id,version_id,source_version_id,role,ordinal) VALUES(p_tenant,identity,(body->''donor''->>''asset_version_id'')::uuid,''glare_donor'',1); END IF;';
 definition:=replace(definition,needle,replacement);
 IF position('open_eyes' IN definition)>0 OR position('OpenEyes' IN definition)>0 THEN RAISE EXCEPTION 'GLARE_FOREIGN_AUTHORITY_REMAINS';END IF;
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.apply_v28_glare_version(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v28_glare_version(uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.apply_v28_glare_version(uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
