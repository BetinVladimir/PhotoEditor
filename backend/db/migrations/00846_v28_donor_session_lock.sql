-- +goose Up
-- Relational source/session gate; caller must independently verify document bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v28_donor_session(p_tenant uuid,p_project uuid,p_shoot_session uuid,
 p_actor uuid,p_auth_session uuid,p_proof jsonb,p_source_faces jsonb,p_donor_faces jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE session_row photo.shoot_sessions%ROWTYPE;part record;binding jsonb;observation_id uuid;face_id uuid;
 version_id uuid;image_id uuid;evidence_id uuid;image_sha text;evidence_sha text;person_id uuid;
BEGIN
 IF jsonb_typeof(p_proof) IS DISTINCT FROM 'object' OR p_proof->>'schema_version' IS DISTINCT FROM '1'
  OR p_proof->>'actor_id' IS DISTINCT FROM p_actor::text
  OR jsonb_typeof(p_proof->'donor') IS DISTINCT FROM 'object'
  OR jsonb_typeof(p_source_faces) IS DISTINCT FROM 'object' OR jsonb_typeof(p_donor_faces) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 person_id:=(p_proof->>'person_id')::uuid;
 IF photo.lock_v28_donor_identity(p_tenant,p_project,person_id,(p_proof->>'consent_id')::uuid,
  p_proof->>'epoch',(p_proof->>'source_observation_id')::uuid,(p_proof->>'donor_observation_id')::uuid,p_actor,p_auth_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO session_row FROM photo.shoot_sessions WHERE tenant_id=p_tenant AND project_id=p_project AND id=p_shoot_session
  AND state IN('open','closed') FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.collections WHERE tenant_id=p_tenant AND project_id=p_project AND id=session_row.collection_id AND deleted_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 -- Version lock order follows UUID order, independent of donor direction.
 FOR part IN SELECT * FROM (VALUES
  (p_proof->'source',p_proof->>'source_observation_id',p_proof->>'source_face_id',p_source_faces),
  (p_proof->'donor',p_proof->>'donor_observation_id',p_proof->>'donor_face_id',p_donor_faces))
  pairs(base,observation,face,evidence) ORDER BY base->>'asset_version_id'
 LOOP
  binding:=part.base;observation_id:=part.observation::uuid;face_id:=part.face::uuid;
  version_id:=(binding->>'asset_version_id')::uuid;image_id:=(binding->'source'->>'artifact_id')::uuid;image_sha:=binding->'source'->>'sha256';
  evidence_id:=(part.evidence->>'artifact_id')::uuid;evidence_sha:=part.evidence->>'sha256';
  IF coalesce(image_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(evidence_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  PERFORM v.id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   JOIN photo.collection_items ci ON ci.tenant_id=v.tenant_id AND ci.collection_id=session_row.collection_id
    AND ci.asset_id=a.id AND ci.asset_version_id=v.id
   JOIN photo.artifacts image ON image.tenant_id=v.tenant_id AND image.id=v.master_artifact_id AND image.sha256=v.content_hash
   JOIN photo.person_observations observation ON observation.tenant_id=v.tenant_id AND observation.project_id=p_project
    AND observation.id=observation_id AND observation.asset_version_id=v.id AND observation.face_id=face_id
    AND observation.person_id=person_id AND observation.confirmed_by IS NOT NULL AND observation.evidence_artifact_id=evidence_id
   JOIN photo.artifacts evidence ON evidence.tenant_id=observation.tenant_id AND evidence.id=evidence_id AND evidence.sha256=evidence_sha
   WHERE v.tenant_id=p_tenant AND v.id=version_id AND v.master_artifact_id=image_id AND v.content_hash=image_sha
    AND v.review_state='accepted' AND a.project_id=p_project AND a.deleted_at IS NULL AND a.upload_state='verified'
    AND image.state='ready' AND image.verified_at IS NOT NULL AND image.media_type='application/json'
    AND image.object_version IS NOT NULL AND image.object_version NOT IN('','null') AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
    AND evidence.state='ready' AND evidence.verified_at IS NOT NULL AND evidence.media_type='application/json' AND evidence.bytes BETWEEN 2 AND 1048576
    AND evidence.object_version IS NOT NULL AND evidence.object_version NOT IN('','null') AND (evidence.expires_at IS NULL OR evidence.expires_at>clock_timestamp())
   FOR SHARE OF v,a,ci,image,observation,evidence;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v28_donor_session(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_donor_session(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v28_donor_session(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
