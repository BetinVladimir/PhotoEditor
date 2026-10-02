-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.finish_ingest_run(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;p photo.ingest_progress%ROWTYPE;expected bigint;successes bigint;failures bigint;next_cursor bigint;final_status text;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested THEN RETURN NULL;END IF;
 SELECT * INTO p FROM photo.ingest_progress WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF p.phase='complete' AND r.status IN ('completed','completed_with_errors','failed') THEN RETURN r.status;END IF;
 IF r.status NOT IN ('queued','running') OR NOT r.graph_sealed OR p.phase NOT IN ('decode','catalog') OR r.revision>=9007199254740991 THEN RETURN NULL;END IF;
 SELECT item_count INTO expected FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id;
 IF expected IS NULL OR expected<>p.item_count
 OR (SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=r.input_snapshot_id)<>expected
 OR (SELECT count(*) FROM photo.ingest_upload_bindings WHERE tenant_id=p_tenant AND run_id=p_run)<>expected
 OR (SELECT count(*) FROM photo.ingest_decode_items WHERE tenant_id=p_tenant AND run_id=p_run)<>expected THEN RETURN NULL;END IF;
 IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND state NOT IN ('succeeded','failed','cancelled','skipped'))
 OR EXISTS(SELECT FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND state<>'closed')
 OR EXISTS(SELECT FROM photo.reviews WHERE tenant_id=p_tenant AND run_id=p_run AND state='pending') THEN RETURN NULL;END IF;
 -- Every snapshot ordinal must have exactly one settled outcome and matching upload state.
 IF EXISTS(SELECT FROM photo.snapshot_items s
 LEFT JOIN photo.ingest_upload_bindings b ON b.tenant_id=s.tenant_id AND b.run_id=p_run AND b.ordinal=s.ordinal
 LEFT JOIN photo.upload_sessions u ON u.tenant_id=b.tenant_id AND u.id=b.upload_id
 LEFT JOIN photo.ingest_catalog_items c ON c.tenant_id=b.tenant_id AND c.run_id=b.run_id AND c.ordinal=b.ordinal
 LEFT JOIN photo.ingest_rejections f ON f.tenant_id=b.tenant_id AND f.run_id=b.run_id AND f.ordinal=b.ordinal
 WHERE s.tenant_id=p_tenant AND s.snapshot_id=r.input_snapshot_id AND
 (b.source_id IS DISTINCT FROM s.artifact_id OR b.source_sha IS DISTINCT FROM s.sha256
 OR ((c.settled_at IS NOT NULL AND f.ordinal IS NULL AND u.state='verified' AND u.verified_version_id=b.version_id)
  OR (c.ordinal IS NULL AND f.ordinal IS NOT NULL AND u.state='rejected' AND u.verified_version_id IS NULL)) IS NOT TRUE)) THEN RETURN NULL;END IF;
 SELECT count(*) INTO successes FROM photo.ingest_catalog_items WHERE tenant_id=p_tenant AND run_id=p_run AND settled_at IS NOT NULL;
 SELECT count(*) INTO failures FROM photo.ingest_rejections WHERE tenant_id=p_tenant AND run_id=p_run;
 IF successes+failures<>expected THEN RETURN NULL;END IF;
 final_status:=CASE WHEN failures=0 THEN 'completed' WHEN successes>0 THEN 'completed_with_errors' ELSE 'failed' END;
 -- Tenant-local cursor is allocated in the same commit; replay emits no event.
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.ingest_progress SET phase='complete' WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET status=final_status,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,p_run,r.created_by,'principal','run.finished',jsonb_build_object('run_id',p_run,'status',final_status,'revision',r.revision+1,'imported_items',successes,'failed_items',failures));
 RETURN final_status;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_ingest_run(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_ingest_run(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_ingest_run(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
