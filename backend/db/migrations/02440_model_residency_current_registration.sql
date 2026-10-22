-- +goose Up
-- Cache hints must remain bound to the current worker model/backend identity.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.report_worker_model_residency(p_worker uuid,p_policy text,p_models jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; n integer; updated integer;
BEGIN
 IF p_worker IS NULL OR p_policy IS NULL OR p_policy !~ '^[0-9a-f]{64}$'
 OR p_models IS NULL OR jsonb_typeof(p_models)<>'array' THEN
  RAISE EXCEPTION 'INVALID_MODEL_RESIDENCY' USING ERRCODE='22023'; END IF;
 IF jsonb_array_length(p_models)>16 THEN RAISE EXCEPTION 'INVALID_MODEL_RESIDENCY' USING ERRCODE='22023';END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_models) LOOP
  IF jsonb_typeof(item)<>'object' OR NOT item ?& ARRAY['model_lock_sha','model_id','backend','qualification_report_sha','generation']
  OR item-'model_lock_sha'-'model_id'-'backend'-'qualification_report_sha'-'generation'<>'{}'::jsonb
  OR jsonb_typeof(item->'model_lock_sha') IS DISTINCT FROM 'string' OR (item->>'model_lock_sha') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(item->'qualification_report_sha') IS DISTINCT FROM 'string' OR (item->>'qualification_report_sha') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(item->'model_id') IS DISTINCT FROM 'string' OR length(item->>'model_id') NOT BETWEEN 1 AND 128
  OR jsonb_typeof(item->'backend') IS DISTINCT FROM 'string' OR item->>'backend' NOT IN('cpu','cuda','mps')
  OR jsonb_typeof(item->'generation') IS DISTINCT FROM 'number' THEN
   RAISE EXCEPTION 'INVALID_MODEL_RESIDENCY' USING ERRCODE='22023'; END IF;
  IF (item->>'generation')::numeric<0 OR (item->>'generation')::numeric>9007199254740991
  OR trunc((item->>'generation')::numeric)<>(item->>'generation')::numeric THEN
   RAISE EXCEPTION 'INVALID_MODEL_RESIDENCY' USING ERRCODE='22023'; END IF;
 END LOOP;
 SELECT count(*) INTO n FROM (SELECT DISTINCT value->>'model_lock_sha',value->>'model_id',value->>'backend' FROM jsonb_array_elements(p_models)) unique_models;
 IF n<>jsonb_array_length(p_models) THEN RAISE EXCEPTION 'DUPLICATE_MODEL_RESIDENCY' USING ERRCODE='22023'; END IF;
 PERFORM 1 FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.enabled AND w.db_principal=session_user FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.enabled AND w.db_principal=session_user
  AND (w.capacity_policy_sha IS DISTINCT FROM p_policy OR NOT photo.capacity_policy_worker_fresh(w))) THEN RETURN false;END IF;
 -- Registration locks above serialize operator lock/backend changes with hints.
 -- A stale pool must not republish a hint withdrawn by authority invalidation.
 IF EXISTS(SELECT 1 FROM photo.worker_registrations w
  CROSS JOIN LATERAL jsonb_array_elements(p_models) model
  WHERE w.worker_id=p_worker AND w.enabled AND w.db_principal=session_user
   AND (w.lock_id IS DISTINCT FROM model->>'model_lock_sha'
    OR NOT coalesce(w.backends ? (model->>'backend'),false))) THEN
  RETURN false;
 END IF;
 UPDATE photo.worker_registrations w SET model_residency=p_models,model_residency_reported_at=clock_timestamp()
 WHERE w.worker_id=p_worker AND w.enabled AND w.db_principal=session_user AND w.capacity_policy_sha=p_policy;
 GET DIAGNOSTICS updated=ROW_COUNT;
 RETURN updated>0;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.report_worker_model_residency(uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.report_worker_model_residency(uuid,text,jsonb) FROM PUBLIC,photo_api,photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.report_worker_model_residency(uuid,text,jsonb) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
