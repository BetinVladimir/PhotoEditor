-- +goose Up
-- V29 main, preview, quality and line Params may become ready before
-- v2_task_inputs exists.
-- Keep only documents attached to their exact live draft; sealed tasks use
-- the existing registered-input reference path.
-- +goose StatementBegin
DO $$
DECLARE body text; updated text;
BEGIN
 SELECT prosrc INTO body FROM pg_proc
 WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]'
    OR strpos(body,'v28_restoration_preview_params')=0
    OR strpos(body,'v29_perspective_preview_params')>0 THEN
  RAISE EXCEPTION 'V29_DRAFT_PARAMS_GC_SHAPE';
 END IF;
 updated:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',
  E'\\1 ' || $gc$
  EXISTS(SELECT FROM photo.backend_documents d
   JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id
   WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact
    AND d.purpose='task_params' AND d.state='ready'
    AND r.kind='v2' AND r.status='draft' AND NOT r.cancel_requested
    AND ((r.config_snapshot->'v29_multi_plan'->>'task_id'=d.owner_id::text
       AND r.config_snapshot->'v29_multi_plan'->>'params_sha'=d.sha256)
      OR (r.config_snapshot->'v29_perspective_plan'->>'task_id'=d.owner_id::text
       AND r.config_snapshot->'v29_perspective_plan'->>'params_sha'=d.sha256)
      OR (r.config_snapshot->'v29_perspective_lines_plan'->>'owner_id'=d.owner_id::text
       AND r.config_snapshot->'v29_perspective_lines_plan'->>'sha256'=d.sha256)
      OR (r.v29_perspective_line_detection IS TRUE
       AND r.config_snapshot->'v29_perspective_line_detection'->>'task_id'=d.owner_id::text
       AND d.sha256=encode(sha256(convert_to(r.config_snapshot->'v29_perspective_line_detection'->>'params_jcs','UTF8')),'hex')))) OR
  EXISTS(SELECT FROM photo.backend_documents d
   JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id
   WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact
    AND d.purpose='task_params' AND d.state='ready'
    AND r.kind='preview' AND r.status='draft' AND NOT r.cancel_requested
    AND ((d.v29_perspective_preview_params IS NOT NULL
       AND r.v29_perspective_preview_task_id IS NOT NULL)
      OR (d.v29_multi_preview_params IS NOT NULL
       AND r.v29_multi_preview_task_id IS NOT NULL)
      OR (d.v29_perspective_quality_params IS NOT NULL
       AND r.v29_perspective_quality_task_id IS NOT NULL)
      OR (d.v29_multi_quality_params IS NOT NULL
       AND r.v29_multi_quality_task_id IS NOT NULL))) OR
  $gc$,'i');
 IF updated=body THEN RAISE EXCEPTION 'V29_DRAFT_PARAMS_GC_PATCH_FAILED';END IF;
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',updated);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
