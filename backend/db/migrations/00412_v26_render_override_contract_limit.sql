-- +goose Up
-- Match EditingRecipeV3 maximum128 overrides instead of imposing32.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_render_direct_inputs_ready(p_tenant uuid,p_task uuid,p_project uuid,p_actor uuid,p_session uuid,p_body jsonb)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ref jsonb; kind text; version_id uuid; pairs jsonb;
BEGIN
 version_id:=(p_body->'scalars'->'source'->>'asset_version_id')::uuid;
 pairs:=jsonb_build_array(jsonb_build_object('ref',p_body->'recipe'->'base_style','contract','StyleV2'));
 IF p_body->'recipe'->'session_corrections' IS NOT NULL AND p_body->'recipe'->'session_corrections'<>'null'::jsonb THEN
  pairs:=pairs||jsonb_build_array(jsonb_build_object('ref',p_body->'recipe'->'session_corrections','contract','CorrectionPlan'));END IF;
 IF p_body->'recipe' ? 'overrides' THEN
  IF jsonb_typeof(p_body->'recipe'->'overrides') IS DISTINCT FROM 'array'
   OR jsonb_array_length(p_body->'recipe'->'overrides')>128 THEN RETURN false;END IF;
  FOR ref IN SELECT value FROM jsonb_array_elements(p_body->'recipe'->'overrides') LOOP
   pairs:=pairs||jsonb_build_array(jsonb_build_object('ref',ref,'contract','Override'));END LOOP;
 END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(pairs) LOOP
  kind:=ref->>'contract';ref:=ref->'ref';
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.contract=kind AND i.artifact_id::text=ref->>'artifact_id' AND i.sha256=ref->>'sha256')
  OR NOT photo.v2_draft_artifact_scope(p_tenant,p_project,version_id,(ref->>'artifact_id')::uuid,ref->>'sha256',kind,p_actor,p_session) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
