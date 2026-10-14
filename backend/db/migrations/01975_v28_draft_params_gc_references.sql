-- +goose Up
-- V28 task Params are published before their frozen v2_task_inputs exist.
-- Retain only a ready document whose owner and digest still match the saved
-- local/donor/relight/restoration intent on the exact draft run.
-- +goose StatementBegin
DO $$
DECLARE body text; updated text;
BEGIN
 SELECT prosrc INTO body FROM pg_proc
 WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]'
    OR strpos(body,'v28_restoration_preview_params')=0
    OR strpos(body,'v28_local_portrait')>0 THEN
  RAISE EXCEPTION 'V28_DRAFT_PARAMS_GC_SHAPE';
 END IF;
 updated:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',
  E'\\1 EXISTS(SELECT FROM photo.backend_documents d JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact AND d.purpose=''task_params'' AND d.state=''ready'' AND r.kind=''v2'' AND r.status=''draft'' AND NOT r.cancel_requested AND ((r.config_snapshot->''v28_local_portrait''->''intent''->>''task_id''=d.owner_id::text AND r.config_snapshot->''v28_local_portrait''->''intent''->>''params_sha''=d.sha256) OR (r.v28_donor_params_intent->>''owner_id''=d.owner_id::text AND r.v28_donor_params_intent->>''params_sha''=d.sha256) OR (r.v28_relight_intent->>''owner_id''=d.owner_id::text AND r.v28_relight_intent->>''params_sha''=d.sha256) OR (r.v28_restoration_params_intent->>''owner_id''=d.owner_id::text AND r.v28_restoration_params_intent->>''params_sha''=d.sha256))) OR ','i');
 IF updated=body THEN RAISE EXCEPTION 'V28_DRAFT_PARAMS_GC_PATCH_FAILED';END IF;
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',updated);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
