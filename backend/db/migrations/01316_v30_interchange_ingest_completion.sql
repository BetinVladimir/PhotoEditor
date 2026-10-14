-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.reject_v30_interchange_ingest(p_tenant uuid,p_actor uuid,p_session uuid,p_run uuid,p_ordinal integer,p_source uuid,p_sha text,p_stage text,p_reason text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;b photo.v30_interchange_ingest_bindings%ROWTYPE;old photo.v30_interchange_ingest_outcomes%ROWTYPE;item photo.ingest_decode_items%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_stage IS NULL OR p_stage NOT IN('asset.verify','asset.inspect','asset.decode') OR p_reason IS NULL OR p_reason NOT IN('failed','cancelled') THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO b FROM photo.v30_interchange_ingest_bindings WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal;
 IF r.id IS NULL OR b.run_id IS NULL OR r.kind IS DISTINCT FROM 'ingest' OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor
 OR b.actor_id IS DISTINCT FROM p_actor OR b.source_id IS DISTINCT FROM p_source OR b.source_sha IS DISTINCT FROM p_sha
 OR photo.v2_editor_authorized(p_tenant,b.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.v30_interchange_ingest_outcomes WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal;
 IF FOUND THEN RETURN old.state='rejected' AND old.failure_stage=p_stage AND old.failure_reason=p_reason;END IF;
 IF r.status NOT IN('queued','running') OR NOT r.graph_sealed OR NOT EXISTS(SELECT 1 FROM photo.ingest_progress WHERE tenant_id=p_tenant AND run_id=p_run AND phase IN('decode','catalog')) THEN RETURN false;END IF;
 SELECT * INTO item FROM photo.ingest_decode_items WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal;
 IF item.run_id IS NULL OR (p_stage IN('asset.verify','asset.inspect') AND (item.outcome IS DISTINCT FROM 'failed' OR item.failure_stage IS DISTINCT FROM p_stage OR item.failure_reason IS DISTINCT FROM p_reason))
 OR (p_stage='asset.decode' AND item.outcome IS DISTINCT FROM 'decode') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.ingest_node_plans n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id
 WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.ordinal=p_ordinal AND n.source_artifact_id=p_source AND n.source_sha=p_sha AND n.operation=p_stage AND t.operation=p_stage AND t.state=p_reason)
 OR EXISTS(SELECT 1 FROM photo.assets WHERE tenant_id=p_tenant AND id=b.asset_id)
 OR EXISTS(SELECT 1 FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=b.version_id) THEN RETURN false;END IF;
 INSERT INTO photo.v30_interchange_ingest_outcomes(tenant_id,run_id,ordinal,state,failure_stage,failure_reason)
 VALUES(p_tenant,p_run,p_ordinal,'rejected',p_stage,p_reason);
 RETURN true;
END $$;
ALTER FUNCTION photo.reject_v30_interchange_ingest(uuid,uuid,uuid,uuid,integer,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reject_v30_interchange_ingest(uuid,uuid,uuid,uuid,integer,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reject_v30_interchange_ingest(uuid,uuid,uuid,uuid,integer,uuid,text,text,text) TO photo_scheduler;

CREATE FUNCTION photo.finish_v30_interchange_ingest(p_tenant uuid,p_actor uuid,p_session uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;p photo.ingest_progress%ROWTYPE;project uuid;expected bigint;successes bigint;failures bigint;next_cursor bigint;final_status text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT project_id INTO project FROM photo.v30_interchange_ingest_bindings WHERE tenant_id=p_tenant AND run_id=p_run LIMIT 1;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'ingest' OR r.created_by IS DISTINCT FROM p_actor OR r.cancel_requested OR project IS NULL
 OR photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO p FROM photo.ingest_progress WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF p.run_id IS NULL THEN RETURN NULL;END IF;
 IF p.phase='complete' AND r.status IN('completed','completed_with_errors','failed') THEN RETURN r.status;END IF;
 IF r.status NOT IN('queued','running') OR NOT r.graph_sealed OR p.phase NOT IN('decode','catalog') OR r.revision>=9007199254740991 THEN RETURN NULL;END IF;
 SELECT item_count INTO expected FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id;
 IF expected IS NULL OR expected NOT BETWEEN 1 AND 5000 OR expected<>p.item_count
 OR (SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=r.input_snapshot_id)<>expected
 OR (SELECT count(*) FROM photo.v30_interchange_ingest_bindings WHERE tenant_id=p_tenant AND run_id=p_run)<>expected
 OR (SELECT count(*) FROM photo.ingest_decode_items WHERE tenant_id=p_tenant AND run_id=p_run)<>expected
 OR (SELECT count(*) FROM photo.v30_interchange_ingest_outcomes WHERE tenant_id=p_tenant AND run_id=p_run)<>expected THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND state NOT IN('succeeded','failed','cancelled','skipped'))
 OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND state<>'closed')
 OR EXISTS(SELECT 1 FROM photo.reviews WHERE tenant_id=p_tenant AND run_id=p_run AND state='pending') THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM photo.snapshot_items s
 LEFT JOIN photo.v30_interchange_ingest_bindings b ON b.tenant_id=s.tenant_id AND b.run_id=p_run AND b.ordinal=s.ordinal
 LEFT JOIN photo.v30_interchange_ingest_outcomes o ON o.tenant_id=b.tenant_id AND o.run_id=b.run_id AND o.ordinal=b.ordinal
 LEFT JOIN photo.asset_versions v ON v.tenant_id=b.tenant_id AND v.id=b.version_id AND v.asset_id=b.asset_id
 LEFT JOIN photo.assets a ON a.tenant_id=b.tenant_id AND a.id=b.asset_id
 WHERE s.tenant_id=p_tenant AND s.snapshot_id=r.input_snapshot_id AND
 (b.source_id IS DISTINCT FROM s.artifact_id OR b.source_sha IS DISTINCT FROM s.sha256 OR b.project_id IS DISTINCT FROM project OR b.actor_id IS DISTINCT FROM p_actor
 OR ((o.state='adopted' AND v.master_artifact_id=o.master_id AND v.content_hash=o.master_sha AND a.original_artifact_id=b.source_id)
 OR(o.state='rejected' AND v.id IS NULL AND a.id IS NULL)) IS NOT TRUE)) THEN RETURN NULL;END IF;
 SELECT count(*) FILTER(WHERE state='adopted'),count(*) FILTER(WHERE state='rejected') INTO successes,failures FROM photo.v30_interchange_ingest_outcomes WHERE tenant_id=p_tenant AND run_id=p_run;
 final_status:=CASE WHEN failures=0 THEN 'completed' WHEN successes>0 THEN 'completed_with_errors' ELSE 'failed' END;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.ingest_progress SET phase='complete' WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET status=final_status,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,p_run,p_actor,'principal','run.finished',jsonb_build_object('run_id',p_run,'status',final_status,'revision',r.revision+1,'imported_items',successes,'failed_items',failures));
 RETURN final_status;
END $$;
ALTER FUNCTION photo.finish_v30_interchange_ingest(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v30_interchange_ingest(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.finish_v30_interchange_ingest(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
