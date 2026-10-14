-- Recheck the current Apply lineage and complete SeriesStats inventory when a
-- frozen training manifest is considered. This owner-only predicate does not
-- remove the series admission gate in 02093; qualification still controls it.
-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE definition text; old_name text; old_source text; old_session text;
BEGIN
 definition:=pg_get_functiondef(
  'photo.v23_series_stat_artifact(uuid,uuid,uuid,uuid,uuid,uuid,text)'::regprocedure);
 old_name:='FUNCTION photo.v23_series_stat_artifact(';
 old_source:='photo.v23_series_event_source(';
 old_session:='p_auth_session IS NULL OR ';
 IF (length(definition)-length(replace(definition,old_name,'')))/length(old_name)<>1 OR
  (length(definition)-length(replace(definition,old_source,'')))/length(old_source)<>1 OR
  (length(definition)-length(replace(definition,old_session,'')))/length(old_session)<>1 THEN
  RAISE EXCEPTION 'Unexpected V23 SeriesStats locator definition';
 END IF;
 definition:=replace(definition,old_name,
  'FUNCTION photo.v23_series_stat_artifact_frozen_core(');
 definition:=replace(definition,old_source,
  'photo.v23_series_event_source_evaluate_core(');
 definition:=replace(definition,old_session,'');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_stat_artifact_frozen_core(
 uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_stat_artifact_frozen_core(
 uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_series_frozen_ready(
 p_tenant uuid,p_run uuid,p_manifest jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE run_row photo.runs%ROWTYPE; project_id uuid; recovery jsonb;
 item jsonb; evidence jsonb; series_entry jsonb; frozen_body jsonb;
 stats_entry jsonb; stats_ref jsonb;
 event_id uuid; artifact_id uuid; expected_id uuid; h bytea;
 source_row record; stat_row record; document photo.backend_documents%ROWTYPE;
 artifact photo.artifacts%ROWTYPE; expected_stats jsonb; seen uuid[];
 series_count integer:=0;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR
  jsonb_typeof(p_manifest->'examples') IS DISTINCT FROM 'array' THEN
  RETURN false; END IF;
 SELECT * INTO run_row FROM photo.runs r WHERE r.tenant_id=p_tenant AND
  r.id=p_run AND r.kind='v2' AND NOT r.cancel_requested FOR SHARE;
 IF NOT FOUND OR run_row.created_by IS NULL THEN RETURN false; END IF;
 SELECT p.project_id INTO project_id FROM photo.learning_profiles p WHERE
  p.tenant_id=p_tenant AND p.id=NULLIF(
   run_row.config_snapshot->'v23_training'->>'profile_id','')::uuid;
 IF project_id IS NULL THEN RETURN false; END IF;
 BEGIN
  SELECT i.response_body INTO STRICT recovery FROM photo.idempotency_records i WHERE
   i.tenant_id=p_tenant AND i.principal_type='user' AND
   i.principal_id=run_row.created_by AND i.route='V2.Admit' AND
   i.resource_id=p_run AND i.response_status IS NULL;
 EXCEPTION WHEN NO_DATA_FOUND OR TOO_MANY_ROWS THEN RETURN false;
 END;
 IF recovery IS NULL OR
  jsonb_typeof(recovery->'series_stats_jcs') IS DISTINCT FROM 'string' OR
  jsonb_typeof(recovery->'series_jcs') IS DISTINCT FROM 'string' OR
  jsonb_typeof(recovery->'manifest_jcs') IS DISTINCT FROM 'string' OR
  jsonb_typeof((recovery->>'series_stats_jcs')::jsonb) IS DISTINCT FROM 'array' OR
  jsonb_typeof((recovery->>'series_jcs')::jsonb) IS DISTINCT FROM 'array' OR
  (recovery->>'manifest_jcs')::jsonb IS DISTINCT FROM p_manifest THEN
  RETURN false; END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_manifest->'examples') LOOP
  IF NOT (item->'render_case' ? 'series_evidence') THEN CONTINUE; END IF;
  series_count:=series_count+1;
  IF item->'render_case' ?| ARRAY['defect_evidence','dust_evidence','detail_evidence'] OR
   jsonb_typeof(item->'render_case'->'series_evidence') IS DISTINCT FROM 'object' OR
   (item->'render_case'->'series_evidence'-'artifact_id'-'sha256')<>'{}'::jsonb THEN
   RETURN false; END IF;
  event_id:=(item->>'event_id')::uuid;
  evidence:=item->'render_case'->'series_evidence';
  IF event_id IS NULL OR evidence->>'sha256' !~ '^[0-9a-f]{64}$' THEN
   RETURN false; END IF;
  h:=public.digest(uuid_send(p_run)||convert_to(
   'v23-series/'||event_id::text,'UTF8'),'sha1');
  h:=set_byte(h,6,(get_byte(h,6)&15)|80);
  h:=set_byte(h,8,(get_byte(h,8)&63)|128);
  expected_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
  IF evidence->>'artifact_id' IS DISTINCT FROM expected_id::text THEN
   RETURN false; END IF;
  SELECT * INTO source_row FROM photo.v23_series_event_source_evaluate_core(
   p_tenant,project_id,run_row.created_by,NULL::uuid,event_id);
  IF NOT FOUND OR source_row.asset_version_id::text IS DISTINCT FROM
   item->'render_case'->'binding'->>'asset_version_id' OR
   item->'render_case'->'working_image' IS DISTINCT FROM
    jsonb_build_object('artifact_id',source_row.source_image_id,
     'sha256',source_row.source_image_sha) THEN RETURN false; END IF;
  BEGIN
   SELECT value INTO STRICT series_entry FROM jsonb_array_elements(
    (recovery->>'series_jcs')::jsonb) x WHERE
    x.value->>'event_id'=event_id::text;
  EXCEPTION WHEN NO_DATA_FOUND OR TOO_MANY_ROWS THEN RETURN false;
  END;
  IF jsonb_typeof(series_entry->'body_jcs') IS DISTINCT FROM 'string' OR
   encode(public.digest(convert_to(series_entry->>'body_jcs','UTF8'),
    'sha256'),'hex') IS DISTINCT FROM evidence->>'sha256' THEN RETURN false; END IF;
  frozen_body:=(series_entry->>'body_jcs')::jsonb;
  IF jsonb_typeof(frozen_body) IS DISTINCT FROM 'object' OR
   frozen_body->>'tenant_id' IS DISTINCT FROM p_tenant::text OR
   frozen_body->>'event_id' IS DISTINCT FROM event_id::text OR
   frozen_body->>'asset_version_id' IS DISTINCT FROM
    source_row.asset_version_id::text OR
   frozen_body->>'apply_run_id' IS DISTINCT FROM
    source_row.apply_run_id::text OR
   frozen_body->>'item_id' IS DISTINCT FROM source_row.item_id::text OR
   frozen_body->>'producer_task_id' IS DISTINCT FROM
    source_row.producer_task_id::text OR
   frozen_body->'source_image' IS DISTINCT FROM
    jsonb_build_object('artifact_id',source_row.source_image_id,
     'sha256',source_row.source_image_sha) OR
   frozen_body->'stage_image' IS DISTINCT FROM
    jsonb_build_object('artifact_id',source_row.stage_image_id,
     'sha256',source_row.stage_image_sha) OR
   frozen_body->'params' IS DISTINCT FROM
    jsonb_build_object('artifact_id',source_row.params_id,
     'sha256',source_row.params_sha) OR
   frozen_body->'series' IS DISTINCT FROM
    jsonb_build_object('artifact_id',source_row.series_id,
     'sha256',source_row.series_sha) THEN RETURN false; END IF;
  SELECT * INTO document FROM photo.backend_documents d WHERE
   d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=event_id AND
   d.purpose='v23_series_holdout' AND d.artifact_id=expected_id AND
   d.sha256=evidence->>'sha256' AND d.state='ready' FOR SHARE;
  IF NOT FOUND THEN RETURN false; END IF;
  SELECT * INTO artifact FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND
   a.id=expected_id AND a.sha256=document.sha256 AND a.state='ready' AND
   a.verified_at IS NOT NULL AND a.output_role='v23_series_holdout' AND
   a.media_type='application/json' AND a.bucket=document.bucket AND
   a.bytes=document.bytes AND a.object_version=document.object_version AND
   a.object_version NOT IN('','null') AND
   a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||expected_id AND
   (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
  IF NOT FOUND THEN RETURN false; END IF;
  BEGIN
   SELECT value INTO STRICT stats_entry FROM jsonb_array_elements(
    (recovery->>'series_stats_jcs')::jsonb) x WHERE
    x.value->>'event_id'=event_id::text;
  EXCEPTION WHEN NO_DATA_FOUND OR TOO_MANY_ROWS THEN RETURN false;
  END;
  SELECT n.params->'stats' INTO expected_stats FROM photo.apply_nodes n WHERE
   n.tenant_id=p_tenant AND n.run_id=source_row.apply_run_id AND
   n.id=source_row.producer_task_id AND n.phase='series_balance' AND
   n.operation='series.balance';
  IF jsonb_typeof(stats_entry->'stats') IS DISTINCT FROM 'array' OR
   jsonb_array_length(stats_entry->'stats') NOT BETWEEN 2 AND 2000 OR
   stats_entry->'stats' IS DISTINCT FROM expected_stats THEN RETURN false; END IF;
  seen:=ARRAY[]::uuid[];
  FOR stats_ref IN SELECT value FROM jsonb_array_elements(stats_entry->'stats') LOOP
   IF jsonb_typeof(stats_ref) IS DISTINCT FROM 'object' OR
    (stats_ref-'artifact_id'-'sha256')<>'{}'::jsonb OR
    stats_ref->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
   artifact_id:=(stats_ref->>'artifact_id')::uuid;
   IF artifact_id IS NULL OR artifact_id=ANY(seen) THEN RETURN false; END IF;
   seen:=array_append(seen,artifact_id);
   SELECT * INTO stat_row FROM photo.v23_series_stat_artifact_frozen_core(
    p_tenant,project_id,run_row.created_by,NULL::uuid,event_id,
    artifact_id,stats_ref->>'sha256');
   IF NOT FOUND OR stat_row.object_version IS NULL OR
    stat_row.object_version IN('','null') THEN RETURN false; END IF;
  END LOOP;
 END LOOP;
 IF series_count=0 OR series_count<>jsonb_array_length(
  (recovery->>'series_stats_jcs')::jsonb) OR
  series_count<>jsonb_array_length((recovery->>'series_jcs')::jsonb) THEN
  RETURN false; END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value OR invalid_json_text THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_frozen_ready(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_frozen_ready(uuid,uuid,jsonb)
 FROM PUBLIC,photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
