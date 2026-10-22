-- +goose Up
-- +goose StatementBegin
-- Deferred to observe the final diagnostic and run state in the same commit:
-- generic cancellation may synchronize job status before its caller finishes.
CREATE FUNCTION photo.emit_download_terminal_event() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE job photo.download_jobs%ROWTYPE;event uuid;next_cursor bigint;body jsonb;
BEGIN
 IF NEW.status IS NOT DISTINCT FROM OLD.status OR NEW.status NOT IN('ready','failed','cancelled','expired') THEN RETURN NULL;END IF;
 SELECT * INTO job FROM photo.download_jobs WHERE tenant_id=NEW.tenant_id AND id=NEW.id;
 -- Multiple changes in one transaction publish only the surviving projection.
 -- Purge may remove the row before deferred triggers execute.
 IF NOT FOUND OR job.status IS DISTINCT FROM NEW.status THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT FROM photo.runs WHERE tenant_id=job.tenant_id AND id=job.run_id AND kind='download'
  AND input_snapshot_id=job.snapshot_id) THEN RAISE EXCEPTION 'DOWNLOAD_EVENT_RUN_INVARIANT' USING ERRCODE='23514';END IF;
 event:=photo.v30_uuid5(job.id,'download:terminal:'||job.status||':1');
 IF EXISTS(SELECT FROM photo.outbox WHERE tenant_id=job.tenant_id AND event_id=event) THEN RETURN NULL;END IF;
 body:=jsonb_build_object('download_job_id',job.id,'run_id',job.run_id,'status',job.status);
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(job.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=job.tenant_id
  AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 -- Guest events are limited to their collection and exact share-session
 -- principal by the existing list_portal_events authority. No URLs/object keys.
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,collection_id,principal_id,audience,type,payload)
 VALUES(job.tenant_id,next_cursor,job.run_id,job.collection_id,job.principal_id,'principal','download_status',body);
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(job.tenant_id,event,'photo.events.v1','download_status',body||jsonb_build_object(
  'schema_version',1,'event_id',event,'type','download_status','tenant_id',job.tenant_id));
 RETURN NULL;
END $$;
ALTER FUNCTION photo.emit_download_terminal_event() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.emit_download_terminal_event() FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
CREATE CONSTRAINT TRIGGER download_terminal_events AFTER UPDATE ON photo.download_jobs
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION photo.emit_download_terminal_event();
-- +goose StatementEnd
-- +goose StatementBegin
-- Retained-cursor discovery must use the same principal scope as returned rows.
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.list_portal_events(bytea,bigint,integer)'::regprocedure);
 needle:='AND e.type IN (''collection_changed'',''upload_status'',''download_status'');';
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'PORTAL_EVENT_MINIMUM_SCOPE_DRIFT';END IF;
 EXECUTE replace(definition,needle,'AND e.type IN (''collection_changed'',''upload_status'',''download_status'') AND (e.principal_id IS NULL OR e.principal_id=ident.session_id);');
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
