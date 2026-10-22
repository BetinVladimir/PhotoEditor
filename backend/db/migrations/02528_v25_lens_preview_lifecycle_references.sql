-- +goose Up
-- Lens draft Params precede task-input materialization; retain their immutable
-- document only while the exact admitted preview run remains live.
ALTER TABLE photo.runs DROP CONSTRAINT runs_v25_lens_preview_kind;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v25_lens_preview_kind CHECK(
 v25_lens_preview_task_id IS NULL OR (kind='preview'
 AND v26_derivative_task_id IS NULL
 AND v29_perspective_preview_task_id IS NULL
 AND v29_perspective_quality_task_id IS NULL
 AND v29_multi_preview_task_id IS NULL
 AND v29_multi_quality_task_id IS NULL
 AND v28_restoration_preview_task_id IS NULL));
-- +goose StatementBegin
DO $$
DECLARE body text; updated text;
BEGIN
 SELECT prosrc INTO body FROM pg_proc
 WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]'
 OR strpos(body,'v29_perspective_preview_params')=0
 OR strpos(body,'v25_lens_preview_params')>0 THEN
  RAISE EXCEPTION 'V25_LENS_PREVIEW_GC_SHAPE';
 END IF;
 updated:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',
  E'\\1 ' || $gc$
 EXISTS(SELECT FROM photo.backend_documents d
 JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id
 WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact
 AND d.purpose='task_params' AND d.state='ready'
 AND d.v25_lens_preview_params IS NOT NULL
 AND r.kind='preview' AND r.status='draft' AND NOT r.cancel_requested
 AND r.v25_lens_preview_task_id IS NOT NULL) OR
 $gc$,'i');
 IF updated=body THEN RAISE EXCEPTION 'V25_LENS_PREVIEW_GC_PATCH_FAILED';END IF;
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',updated);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
