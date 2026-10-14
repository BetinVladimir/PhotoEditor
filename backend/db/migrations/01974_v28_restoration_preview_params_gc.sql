-- +goose Up
-- A ready preview Params document can precede v2_task_inputs registration.
-- Keep its object while the exact draft preview run is still live; after
-- activation the ordinary task-input reference owns retention instead.
-- +goose StatementBegin
DO $$
DECLARE body text; updated text;
BEGIN
 SELECT prosrc INTO body FROM pg_proc
 WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]'
    OR strpos(body,'photo.v2_task_inputs')=0
    OR strpos(body,'v28_restoration_preview_params')>0 THEN
  RAISE EXCEPTION 'V28_RESTORATION_PREVIEW_PARAMS_GC_SHAPE';
 END IF;
 updated:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',
  E'\\1 EXISTS(SELECT FROM photo.backend_documents d JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact AND d.purpose=''task_params'' AND d.state=''ready'' AND d.v28_restoration_preview_params IS NOT NULL AND r.kind=''preview'' AND r.status=''draft'' AND r.v28_restoration_preview_task_id IS NOT NULL) OR ','i');
 IF updated=body THEN RAISE EXCEPTION 'V28_RESTORATION_PREVIEW_PARAMS_GC_PATCH_FAILED';END IF;
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',updated);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
