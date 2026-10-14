-- Re-evaluate the live Apply series lineage under a fenced Evaluate lease.
-- Interactive editor authorization is replaced only inside this owner-only
-- source clone; the training run actor, project, consent and producer checks
-- remain intact. The series freeze gate is unchanged.
-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE definition text; old_name text; old_session text; old_editor text;
BEGIN
 definition:=pg_get_functiondef(
  'photo.v23_series_event_source(uuid,uuid,uuid,uuid,uuid)'::regprocedure);
 old_name:='FUNCTION photo.v23_series_event_source(';
 old_session:='p_auth_session IS NULL OR ';
 old_editor:='IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,
  p_actor,p_auth_session) THEN RETURN; END IF;';
 IF (length(definition)-length(replace(definition,old_name,'')))/length(old_name)<>1 OR
  (length(definition)-length(replace(definition,old_session,'')))/length(old_session)<>1 OR
  (length(definition)-length(replace(definition,old_editor,'')))/length(old_editor)<>1 THEN
  RAISE EXCEPTION 'Unexpected V23 series source definition';
 END IF;
 definition:=replace(definition,old_name,
  'FUNCTION photo.v23_series_event_source_evaluate_core(');
 definition:=replace(definition,old_session,'');
 definition:=replace(definition,old_editor,'IF NOT FOUND THEN RETURN; END IF;');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_event_source_evaluate_core(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_event_source_evaluate_core(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_series_authority(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,
 p_event uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(asset_version_id uuid,source_id uuid,source_sha text,
 apply_run_id uuid,item_id uuid,producer_task_id uuid,
 stage_id uuid,stage_sha text,params_id uuid,params_sha text,
 series_id uuid,series_sha text,bucket text,object_key text,
 object_version text,bytes bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_row record; run_row photo.runs%ROWTYPE; project_id uuid;
 fit_id uuid; h bytea; expected_id uuid;
 evidence photo.artifacts%ROWTYPE; document photo.backend_documents%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_event IS NULL OR p_artifact IS NULL OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN; END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-series/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_artifact<>expected_id THEN RETURN; END IF;
 SELECT r.* INTO run_row FROM photo.runs r WHERE r.tenant_id=p_tenant AND
  r.id=p_run AND r.kind='v2' AND r.status='running' AND
  NOT r.cancel_requested AND NOT r.graph_sealed FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT e.parent_task_id INTO fit_id FROM photo.graph_expansions e WHERE
  e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_expansion AND
  e.kind='v2.learning.evaluate/1' AND e.state='open' AND
  e.expansion_token=p_token AND
  e.expansion_lease_until>clock_timestamp();
 IF fit_id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE
  t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=fit_id AND
  t.operation='learning.fit' AND t.operation_version='1' AND
  t.state='succeeded' AND t.canonical_result_id IS NOT NULL) OR
  NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.task_id=fit_id AND
   i.artifact_id=p_artifact AND i.sha256=p_sha AND
   i.contract='FrozenSeriesHoldoutEvidence') THEN RETURN; END IF;
 SELECT * INTO evidence FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND
  a.id=p_artifact AND a.sha256=p_sha AND a.state='ready' AND
  a.verified_at IS NOT NULL AND a.output_role='v23_series_holdout' AND
  a.media_type='application/json' AND a.object_version IS NOT NULL AND
  a.object_version NOT IN ('','null') AND a.bytes BETWEEN 2 AND 1048576 AND
  (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO document FROM photo.backend_documents d WHERE
  d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_event AND
  d.purpose='v23_series_holdout' AND d.artifact_id=p_artifact AND
  d.sha256=p_sha AND d.state='ready' AND
  d.object_version=evidence.object_version AND d.bucket=evidence.bucket AND
  d.bytes=evidence.bytes FOR SHARE;
 IF NOT FOUND OR evidence.object_key IS DISTINCT FROM
  'tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact THEN RETURN; END IF;
 SELECT p.project_id INTO project_id FROM photo.learning_profiles p WHERE
  p.tenant_id=p_tenant AND p.id=NULLIF(
   run_row.config_snapshot->'v23_training'->>'profile_id','')::uuid;
 IF project_id IS NULL THEN RETURN; END IF;
 SELECT * INTO source_row FROM photo.v23_series_event_source_evaluate_core(
  p_tenant,project_id,run_row.created_by,NULL::uuid,p_event);
 IF NOT FOUND OR source_row.asset_version_id IS NULL OR
  source_row.source_image_id IS NULL OR source_row.stage_image_id IS NULL OR
  source_row.params_id IS NULL OR source_row.series_id IS NULL THEN RETURN; END IF;
 RETURN QUERY SELECT source_row.asset_version_id,
  source_row.source_image_id,source_row.source_image_sha,
  source_row.apply_run_id,source_row.item_id,source_row.producer_task_id,
  source_row.stage_image_id,source_row.stage_image_sha,
  source_row.params_id,source_row.params_sha,
  source_row.series_id,source_row.series_sha,
  evidence.bucket,evidence.object_key,evidence.object_version,evidence.bytes;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_series_authority(
 uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_series_authority(
 uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_series_authority(
 uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
