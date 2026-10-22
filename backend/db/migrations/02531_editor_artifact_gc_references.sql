-- +goose Up
-- Durable editor bridge/build references must participate in the same GC
-- predicate and artifact row-lock protocol as registered task/document inputs.
CREATE TRIGGER ready_artifact_reference BEFORE INSERT OR UPDATE ON photo.editor_recipe_bridges
 FOR EACH ROW EXECUTE FUNCTION photo.guard_ready_artifact_reference('override_artifact_id');
CREATE TRIGGER ready_artifact_reference BEFORE INSERT OR UPDATE ON photo.editor_effect_builds
 FOR EACH ROW EXECUTE FUNCTION photo.guard_ready_artifact_reference(
  'source_artifact_id','frozen_recipe_artifact_id','executed_style_artifact_id',
  'processed_source_artifact_id','quality_artifact_id','result_artifact_id',
  'seal_params_artifact_id','seal_receipt_artifact_id','effect_source_artifact_id',
  'effect_source_result_artifact_id','final_recipe_artifact_id');
-- +goose StatementBegin
DO $$
DECLARE body text; updated text;
BEGIN
 SELECT prosrc INTO body FROM pg_proc
 WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]'
 OR strpos(body,'photo.editor_effect_builds')>0
 OR strpos(body,'photo.editor_recipe_bridges')>0 THEN
  RAISE EXCEPTION 'EDITOR_ARTIFACT_GC_SOURCE_SHAPE';
 END IF;
 updated:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',
 E'\\1 ' || $gc$
 EXISTS(SELECT FROM photo.editor_recipe_bridges b
  WHERE b.tenant_id=p_tenant AND b.override_artifact_id=p_artifact) OR
 EXISTS(SELECT FROM photo.editor_effect_builds b
  WHERE b.tenant_id=p_tenant AND p_artifact IN(b.source_artifact_id,
   b.frozen_recipe_artifact_id,b.executed_style_artifact_id,
   b.processed_source_artifact_id,b.quality_artifact_id,b.result_artifact_id,
   b.seal_params_artifact_id,b.seal_receipt_artifact_id,b.effect_source_artifact_id,
   b.effect_source_result_artifact_id,b.final_recipe_artifact_id)) OR
 $gc$,'i');
 IF updated=body THEN RAISE EXCEPTION 'EDITOR_ARTIFACT_GC_PATCH_FAILED';END IF;
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',updated);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
