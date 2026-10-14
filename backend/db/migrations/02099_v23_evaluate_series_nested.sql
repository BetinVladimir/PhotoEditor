-- Fenced nested JSON locator for the same immutable Apply series and its
-- complete Stats inventory. Every requested ref must also be a Fit input.
-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE definition text; old_name text; old_session text; old_source text;
BEGIN
 definition:=pg_get_functiondef(
  'photo.v23_series_stat_artifact(uuid,uuid,uuid,uuid,uuid,uuid,text)'::regprocedure);
 old_name:='FUNCTION photo.v23_series_stat_artifact(';
 old_session:='p_auth_session IS NULL OR ';
 old_source:='photo.v23_series_event_source(';
 IF (length(definition)-length(replace(definition,old_name,'')))/length(old_name)<>1 OR
  (length(definition)-length(replace(definition,old_session,'')))/length(old_session)<>1 OR
  (length(definition)-length(replace(definition,old_source,'')))/length(old_source)<>1 THEN
  RAISE EXCEPTION 'Unexpected V23 series stat locator definition';
 END IF;
 definition:=replace(definition,old_name,
  'FUNCTION photo.v23_series_stat_artifact_evaluate_core(');
 definition:=replace(definition,old_session,'');
 definition:=replace(definition,old_source,
  'photo.v23_series_event_source_evaluate_core(');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_stat_artifact_evaluate_core(
 uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_stat_artifact_evaluate_core(
 uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_series_nested(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_event uuid,
 p_evidence uuid,p_evidence_sha text,p_role text,p_artifact uuid,p_sha text)
RETURNS TABLE(item_id uuid,image_id uuid,image_sha text,bucket text,
 object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE origin record; stat_row record; a photo.artifacts%ROWTYPE;
 fit_id uuid; project_id uuid; actor_id uuid; expected_contract text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_event IS NULL OR p_evidence IS NULL OR
  p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR
  p_role NOT IN ('stage_image','params','series','stats') THEN RETURN; END IF;
 SELECT * INTO origin FROM photo.v23_evaluate_series_authority(
  p_tenant,p_run,p_expansion,p_token,p_event,p_evidence,p_evidence_sha);
 IF NOT FOUND THEN RETURN; END IF;
 expected_contract:=CASE p_role WHEN 'stage_image' THEN 'WorkingImage'
  WHEN 'params' THEN 'SeriesParams' WHEN 'series' THEN 'SeriesCorrections'
  ELSE 'SeriesStats' END;
 SELECT e.parent_task_id INTO fit_id FROM photo.graph_expansions e WHERE
  e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_expansion AND
  e.expansion_token=p_token AND e.state='open' AND
  e.expansion_lease_until>clock_timestamp();
 IF fit_id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
  i.tenant_id=p_tenant AND i.task_id=fit_id AND
  i.artifact_id=p_artifact AND i.sha256=p_sha AND
  i.contract=expected_contract) THEN RETURN; END IF;
 IF p_role='stage_image' AND (origin.stage_id IS DISTINCT FROM p_artifact OR
    origin.stage_sha IS DISTINCT FROM p_sha) THEN RETURN; END IF;
 IF p_role='params' AND (origin.params_id IS DISTINCT FROM p_artifact OR
    origin.params_sha IS DISTINCT FROM p_sha) THEN RETURN; END IF;
 IF p_role='series' AND (origin.series_id IS DISTINCT FROM p_artifact OR
    origin.series_sha IS DISTINCT FROM p_sha) THEN RETURN; END IF;
 IF p_role='stats' THEN
  SELECT r.created_by,p.project_id INTO actor_id,project_id FROM photo.runs r
   JOIN photo.learning_profiles p ON p.tenant_id=r.tenant_id AND
    p.id=NULLIF(r.config_snapshot->'v23_training'->>'profile_id','')::uuid
   WHERE r.tenant_id=p_tenant AND r.id=p_run;
  IF actor_id IS NULL OR project_id IS NULL THEN RETURN; END IF;
  SELECT * INTO stat_row FROM photo.v23_series_stat_artifact_evaluate_core(
   p_tenant,project_id,actor_id,NULL::uuid,p_event,p_artifact,p_sha);
  IF NOT FOUND THEN RETURN; END IF;
  RETURN QUERY SELECT stat_row.item_id,stat_row.image_id,
   stat_row.image_sha,stat_row.bucket,stat_row.object_key,
   stat_row.object_version,stat_row.bytes,stat_row.media_type;
  RETURN;
 END IF;
 SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND
  x.id=p_artifact AND x.sha256=p_sha AND x.state='ready' AND
  x.verified_at IS NOT NULL AND x.object_version IS NOT NULL AND
  x.object_version NOT IN ('','null') AND
  x.media_type='application/json' AND x.bytes BETWEEN 2 AND 1048576 AND
  (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 RETURN QUERY SELECT NULL::uuid,NULL::uuid,NULL::text,a.bucket,a.object_key,
  a.object_version,a.bytes,a.media_type;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_series_nested(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_series_nested(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_series_nested(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
