-- +goose Up
-- Reader compatibility for V32 batch queue metadata on original V27 evidence.
-- Preserve the complete config/hash binding, legacy absence, function OIDs/ACLs,
-- and all existing authority, qualification, registration and graph checks.
-- +goose StatementBegin
DO $migration$
DECLARE signature text; definition text; needle text; replacement text; seen integer;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
 'photo.lock_semantic_native_admission_evidence(uuid,uuid,jsonb)',
 'photo.lock_semantic_native_completed_evidence(uuid,uuid,jsonb)',
 'photo.lock_semantic_face_admission_evidence(uuid,uuid,jsonb)',
 'photo.lock_semantic_face_completed_evidence(uuid,uuid,jsonb)',
 'photo.lock_semantic_target_person_admission_evidence(uuid,uuid,jsonb)',
 'photo.lock_semantic_target_person_sealed_evidence(uuid,uuid,jsonb)',
 'photo.lock_semantic_target_detector_admission_evidence(uuid,uuid,jsonb)',
 'photo.lock_semantic_target_detector_sealed_evidence(uuid,uuid,jsonb)',
 'photo.lock_semantic_target_face_admission_evidence(uuid,uuid,jsonb)',
 'photo.lock_semantic_target_face_sealed_evidence(uuid,uuid,jsonb)'
 ] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  seen:=0;
  FOREACH needle IN ARRAY ARRAY[
   $source$(config-ARRAY['project_id',config_key])<>'{}'$source$,
   $source$(config-ARRAY['project_id','v27_face_prepare'])<>'{}'$source$,
   $source$(config-ARRAY['project_id','v27_target_face_prepare'])<>'{}'$source$
  ] LOOP
   IF strpos(definition,needle)>0 THEN
    IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
     RAISE EXCEPTION 'Ambiguous native queue evidence predicate: %',signature;
    END IF;
    replacement:=replace(needle,'ARRAY[','ARRAY[''queue_class'',')
     || $source$ OR (config ? 'queue_class' AND config->'queue_class' IS DISTINCT FROM '"batch"'::jsonb)$source$;
    definition:=replace(definition,needle,replacement);
    seen:=seen+1;
   END IF;
  END LOOP;
  IF seen<>1 THEN RAISE EXCEPTION 'Unexpected native queue evidence shape: %',signature;END IF;
  EXECUTE definition;
 END LOOP;
END $migration$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
