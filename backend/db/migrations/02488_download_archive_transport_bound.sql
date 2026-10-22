-- +goose Up
-- Preserve historical immutable plans; enforce the current wire bound on new rows.
ALTER TABLE photo.download_archive_plans ADD CONSTRAINT download_archive_transport_inputs
 CHECK(jsonb_array_length(inputs) BETWEEN 1 AND 512) NOT VALID;
-- +goose StatementBegin
DO $$ DECLARE source text;anchor text;BEGIN
 source:=pg_get_functiondef('photo.register_download_archive_plan(uuid,uuid,uuid,integer,text,jsonb,jsonb,text,jsonb)'::regprocedure);
 anchor:='jsonb_array_length(p_inputs) NOT BETWEEN 1 AND 5000';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'ARCHIVE_REGISTER_TRANSPORT_DRIFT';END IF;
 EXECUTE replace(source,anchor,'jsonb_array_length(p_inputs) NOT BETWEEN 1 AND 512');
 source:=pg_get_functiondef('photo.materialize_download_archive_task(uuid,uuid,jsonb)'::regprocedure);
 anchor:='OR p_task->>''operation_version''<>''1''';
 IF (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'ARCHIVE_MATERIALIZE_TRANSPORT_DRIFT';END IF;
 EXECUTE replace(source,anchor,'OR jsonb_array_length(n.inputs)>512 '||anchor);
END $$;
-- +goose StatementEnd
-- Oversized historical graphs cannot be rewritten under immutable task IDs.
-- Ordinary cancellation drains attempts/resources; retain all user files,
-- snapshot and selection so the photographer can explicitly request a new job.
-- +goose StatementBegin
DO $$ DECLARE c record;r photo.runs%ROWTYPE;j photo.download_jobs%ROWTYPE;BEGIN
 FOR c IN SELECT DISTINCT tenant_id,run_id FROM photo.download_archive_plans
 WHERE jsonb_array_length(inputs)>512 ORDER BY tenant_id,run_id LOOP
  -- Match current portal cancellation and finalization (02476): run → job.
  SELECT * INTO r FROM photo.runs WHERE tenant_id=c.tenant_id AND id=c.run_id FOR UPDATE;
  SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=c.tenant_id AND run_id=c.run_id FOR UPDATE;
  IF r.kind='download' AND r.status IN('queued','running') AND j.status IN('queued','preparing','building','uploading') THEN
   UPDATE photo.download_jobs SET cancel_requested=true,failure=jsonb_build_object('code','DOWNLOAD_ARCHIVE_FAILED','items',(
    SELECT jsonb_agg(jsonb_build_object('ordinal',i.ordinal,'asset_version_id',i.asset_version_id,'code','ARCHIVE_FAILED') ORDER BY i.ordinal)
    FROM photo.download_job_items i WHERE i.tenant_id=j.tenant_id AND i.download_job_id=j.id))
   WHERE tenant_id=j.tenant_id AND id=j.id;
   UPDATE photo.runs SET status='cancelling',cancel_requested=true,revision=revision+1,
    download_build_token=NULL,download_build_until=NULL WHERE tenant_id=r.tenant_id AND id=r.id;
  END IF;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
