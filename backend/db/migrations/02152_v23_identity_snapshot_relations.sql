-- +goose Up
-- Independently validate the ConsentSnapshot shape at the SQL activation
-- boundary. The scheduler must not be able to bypass Go's face/grant checks.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_identity_snapshot_relations_ready(p_body text)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;grant_item jsonb;face_item jsonb;match_grant jsonb;
 grants_by_id jsonb:='{}'::jsonb;seen_grants text[]:=ARRAY[]::text[];
 seen_faces text[]:=ARRAY[]::text[];face_key text;
 uuid_pattern constant text:='^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
  THEN RETURN false;END IF;
 BEGIN body:=p_body::jsonb;
 EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value
  THEN RETURN false;END;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR
  body->'schema_version' IS DISTINCT FROM '1'::jsonb OR
  body-ARRAY['schema_version','grants','confirmed_faces']<>'{}'::jsonb OR
  jsonb_typeof(body->'grants') IS DISTINCT FROM 'array' OR
  jsonb_typeof(body->'confirmed_faces') IS DISTINCT FROM 'array' OR
  jsonb_array_length(body->'grants')>5000 OR
  jsonb_array_length(body->'confirmed_faces')>12800 THEN RETURN false;END IF;
 FOR grant_item IN SELECT value FROM jsonb_array_elements(body->'grants') LOOP
  IF jsonb_typeof(grant_item) IS DISTINCT FROM 'object' OR
   grant_item-ARRAY['consent_id','epoch','person_id','purpose']<>'{}'::jsonb OR
   NOT (grant_item ?& ARRAY['consent_id','epoch','person_id','purpose']) OR
   jsonb_typeof(grant_item->'consent_id') IS DISTINCT FROM 'string' OR
   grant_item->>'consent_id' !~ uuid_pattern OR
   jsonb_typeof(grant_item->'epoch') IS DISTINCT FROM 'string' OR
   grant_item->>'epoch' !~ '^[1-9][0-9]{0,18}$' OR
   (length(grant_item->>'epoch')=19 AND
    grant_item->>'epoch'>'9223372036854775807') OR
   jsonb_typeof(grant_item->'purpose') IS DISTINCT FROM 'string' OR
   grant_item->>'purpose' NOT IN
    ('identity_matching','learning_person','learning_photographer') OR
   (grant_item->>'purpose'='learning_photographer' AND
    grant_item->'person_id' IS DISTINCT FROM 'null'::jsonb) OR
   (grant_item->>'purpose'<>'learning_photographer' AND
    (jsonb_typeof(grant_item->'person_id') IS DISTINCT FROM 'string' OR
     grant_item->>'person_id' !~ uuid_pattern)) OR
   grant_item->>'consent_id'=ANY(seen_grants) THEN RETURN false;END IF;
  seen_grants:=array_append(seen_grants,grant_item->>'consent_id');
  IF grant_item->>'purpose'='identity_matching' THEN
   grants_by_id:=jsonb_set(grants_by_id,ARRAY[grant_item->>'consent_id'],grant_item);
  END IF;
 END LOOP;
 FOR face_item IN SELECT value FROM jsonb_array_elements(body->'confirmed_faces') LOOP
  IF jsonb_typeof(face_item) IS DISTINCT FROM 'object' OR
   face_item-ARRAY['asset_version_id','face_id','person_id','consent_id','epoch']<>'{}'::jsonb OR
   NOT (face_item ?& ARRAY['asset_version_id','face_id','person_id','consent_id','epoch']) OR
   jsonb_typeof(face_item->'asset_version_id') IS DISTINCT FROM 'string' OR
   jsonb_typeof(face_item->'face_id') IS DISTINCT FROM 'string' OR
   jsonb_typeof(face_item->'person_id') IS DISTINCT FROM 'string' OR
   jsonb_typeof(face_item->'consent_id') IS DISTINCT FROM 'string' OR
   jsonb_typeof(face_item->'epoch') IS DISTINCT FROM 'string' OR
   face_item->>'asset_version_id' !~ uuid_pattern OR
   face_item->>'face_id' !~ uuid_pattern OR
   face_item->>'person_id' !~ uuid_pattern OR
   face_item->>'consent_id' !~ uuid_pattern OR
   face_item->>'epoch' !~ '^[1-9][0-9]{0,18}$' OR
   (length(face_item->>'epoch')=19 AND
    face_item->>'epoch'>'9223372036854775807') THEN RETURN false;END IF;
  match_grant:=grants_by_id->(face_item->>'consent_id');
  face_key:=face_item->>'asset_version_id'||':'||face_item->>'face_id';
  IF match_grant IS NULL OR
   match_grant->>'person_id' IS DISTINCT FROM face_item->>'person_id' OR
   match_grant->>'epoch' IS DISTINCT FROM face_item->>'epoch' OR
   face_key=ANY(seen_faces) THEN RETURN false;END IF;
  seen_faces:=array_append(seen_faces,face_key);
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.v23_identity_snapshot_relations_ready(text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_identity_snapshot_relations_ready(text) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.activate_v23_identity_compare(p_tenant uuid,p_run uuid,
 p_workflow uuid,p_task uuid,p_consents text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_workflow IS NULL OR p_task IS NULL
  OR photo.v23_identity_snapshot_relations_ready(p_consents) IS DISTINCT FROM true
  THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1 OR
  NOT EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.tasks t
   ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id
   WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.id=p_workflow
    AND t.id=p_task AND t.operation='identity.compare'
    AND t.operation_version='1' AND t.resource_class='gpu')
   THEN RETURN false;END IF;
 IF photo.pin_v23_identity_compare_consents(p_tenant,p_workflow,p_task,p_consents)
   IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v23_identity(p_tenant,p_run);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
