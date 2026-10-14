-- +goose Up
-- Successful export must prove its dedicated adoption, then use the existing
-- V2 finalizer for terminal events, quota release and cancellation semantics.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_export_committed(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;z photo.task_results%ROWTYPE;
 proof photo.v30_interchange_export_evidence%ROWTYPE;e jsonb;marker uuid;receipt jsonb;refs jsonb;ref jsonb;source jsonb;
 link_ordinal integer:=0;source_index integer:=0;purpose_path text;expected_purpose text;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND workflow_type='interchange' AND renderer_version='v2.2';
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('running','completed') OR NOT r.graph_sealed OR r.cancel_requested
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1 THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=photo.v30_uuid5(w.id,'v2.interchange.export:0') FOR UPDATE;
 IF t.id IS NULL OR t.operation<>'v2.interchange.export' OR t.operation_version<>'1' OR t.state<>'succeeded' OR t.cancel_requested OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=t.canonical_result_id AND attempt=t.attempt AND fencing_token=t.fencing_token FOR SHARE;
 SELECT * INTO proof FROM photo.v30_interchange_export_evidence WHERE tenant_id=p_tenant AND run_id=p_run AND result_id=t.canonical_result_id FOR SHARE;
 IF z.id IS NULL OR z.review_required OR proof.run_id IS NULL OR encode(sha256(convert_to(proof.evidence_jcs,'UTF8')),'hex') IS DISTINCT FROM proof.evidence_sha THEN RETURN false;END IF;
 e:=proof.evidence_jcs::jsonb;marker:=photo.v2_adoption_event_id(t.id,z.id);
 IF e->'root' IS DISTINCT FROM z.outputs->0 OR e->'identity'->>'Run' IS DISTINCT FROM p_run::text
 OR e->'identity'->>'Workflow' IS DISTINCT FROM w.id::text OR e->'identity'->>'Task' IS DISTINCT FROM t.id::text
 OR e->>'request_sha256' IS DISTINCT FROM r.config_snapshot->>'request_sha'
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt' AND event_id=marker)
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-interchange-export' AND event_id=marker) THEN RETURN false;END IF;
 SELECT payload INTO receipt FROM photo.run_events WHERE tenant_id=p_tenant AND run_id=p_run AND type='v2.task.adopted'
 AND payload->>'task_id'=t.id::text AND payload->>'result_id'=z.id::text AND payload->>'attempt'=t.attempt::text AND payload->>'fence'=t.fencing_token::text;
 IF receipt IS NULL OR receipt->>'export_evidence_sha' IS DISTINCT FROM proof.evidence_sha OR receipt->>'inventory_digest' IS DISTINCT FROM proof.evidence_sha THEN RETURN false;END IF;
 refs:=(e->'documents')||jsonb_build_array(e->'bundle');
 IF jsonb_array_length(refs)<>jsonb_array_length(z.outputs) OR NOT refs @> z.outputs OR NOT z.outputs @> refs THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(refs) LOOP
  purpose_path:=CASE WHEN link_ordinal=0 THEN '$' WHEN link_ordinal=1 THEN '$/report' WHEN link_ordinal=jsonb_array_length(refs)-1 THEN '$/bundle' ELSE '$/report/shards/'||(link_ordinal-2)::text END;
  expected_purpose:=encode(sha256(convert_to('tasks/'||t.id||'/'||purpose_path,'UTF8')),'hex');
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' FOR SHARE;
  IF a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
  OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) OR a.created_by_task_id IS DISTINCT FROM t.id
  OR a.produced_attempt IS DISTINCT FROM z.attempt OR a.produced_fence IS DISTINCT FROM z.fencing_token
  OR NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links WHERE tenant_id=p_tenant AND project_id=w.project_id AND workflow_id=w.id
  AND artifact_id=a.id AND sha256=a.sha256 AND purpose=expected_purpose AND ordinal=link_ordinal) THEN RETURN false;END IF;
  link_ordinal:=link_ordinal+1;
 END LOOP;
 FOR source IN SELECT value FROM jsonb_array_elements(e->'sources') LOOP
  ref:=source->'ref';expected_purpose:=encode(sha256(convert_to('tasks/'||t.id||'/$/portable_sources/'||source_index::text,'UTF8')),'hex');
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
   JOIN photo.v2_artifact_links l ON l.tenant_id=a.tenant_id AND l.artifact_id=a.id AND l.sha256=a.sha256
   WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(ref->>'artifact_id')::uuid AND i.sha256=ref->>'sha256' AND i.contract=source->>'contract'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND l.project_id=w.project_id AND l.workflow_id=w.id AND l.ordinal=link_ordinal AND l.purpose=expected_purpose) THEN RETURN false;END IF;
  source_index:=source_index+1;link_ordinal:=link_ordinal+1;
 END LOOP;
 RETURN (SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=p_tenant AND workflow_id=w.id)=link_ordinal
 AND receipt->>'inventory_count'=link_ordinal::text;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_interchange_export_committed(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_export_committed(uuid,uuid) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) RENAME TO finalize_v2_run_pre_export_finalization;
REVOKE ALL ON FUNCTION photo.finalize_v2_run_pre_export_finalization(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.finalize_v2_run(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF r.status='running' AND NOT r.cancel_requested AND EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='v2.interchange.export')
 AND NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND state IN('failed','cancelled','skipped')) THEN
  IF photo.v30_interchange_export_committed(p_tenant,p_run) IS DISTINCT FROM true THEN RETURN 'pending';END IF;
  -- Skip only the interchange success barrier; all earlier V2 lifecycle gates
  -- and quota accounting remain in the existing finalizer chain.
  RETURN photo.finalize_v2_run_pre_interchange(p_tenant,p_run);
 END IF;
 RETURN photo.finalize_v2_run_pre_export_finalization(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v2_run(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_v2_run(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
