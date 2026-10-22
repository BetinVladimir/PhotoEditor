-- +goose Up
-- Scheduler-only live decision gate. Archived review bytes are never trusted
-- as acceptance. A pending or revoked decision returns no row.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_review_live_decision(
 p_tenant uuid,p_workflow uuid,p_entry uuid,p_target uuid,p_kind text,
 p_proposal uuid,p_proposal_sha text,p_base uuid,p_base_sha text,
 p_label uuid,p_label_sha text,p_parsed text)
RETURNS TABLE(decision text,accepted_ids jsonb,rejected_ids jsonb,epoch bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;sources jsonb;source jsonb;
 d photo.v30_import_review_decisions%ROWTYPE;scope jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_workflow IS NULL OR p_entry IS NULL OR p_target IS NULL OR p_proposal IS NULL OR p_base IS NULL
 OR p_kind IS NULL OR p_kind NOT IN('defects','dust') OR coalesce(p_proposal_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_base_sha,'') !~ '^[0-9a-f]{64}$'
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608 THEN RETURN;END IF;
 IF p_kind='defects' THEN
  IF p_label IS NULL OR coalesce(p_label_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 ELSIF p_label IS NOT NULL OR p_label_sha IS NOT NULL THEN RETURN;END IF;
 PERFORM 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant AND x.id=p_workflow
  AND x.workflow_type='interchange' AND x.renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.id=w.run_id FOR SHARE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN;END IF;
 SELECT e.value INTO source FROM jsonb_array_elements(sources->'entries') e
  WHERE e.value->>'entry_id'=p_entry::text AND e.value->>'state'='adopted'
  AND e.value->>'asset_version_id'=p_target::text;
 IF source IS NULL OR source#>>'{master,artifact_id}' IS DISTINCT FROM p_base::text
 OR source#>>'{master,sha256}' IS DISTINCT FROM p_base_sha THEN RETURN;END IF;
 SELECT b.v30_import_prepared INTO scope FROM photo.backend_documents b
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=p_tenant AND b.run_id=w.run_id AND b.artifact_id=p_proposal
  AND b.purpose='v30_import_prepared' AND b.state='ready' AND b.sha256=p_proposal_sha
  AND a.state='ready' AND a.sha256=p_proposal_sha AND a.output_role='v30_import_prepared'
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF scope IS NULL OR scope->>'contract' IS DISTINCT FROM (CASE WHEN p_kind='defects' THEN 'DefectCandidates' ELSE 'DustCandidates' END)
 OR scope->>'workflow' IS DISTINCT FROM p_workflow::text OR scope->>'entry' IS DISTINCT FROM p_entry::text
 OR scope#>>'{binding,asset_version_id}' IS DISTINCT FROM p_target::text
 OR scope#>>'{binding,source,artifact_id}' IS DISTINCT FROM p_base::text
 OR scope#>>'{binding,source,sha256}' IS DISTINCT FROM p_base_sha THEN RETURN;END IF;
 SELECT * INTO d FROM photo.v30_import_review_decisions x WHERE x.tenant_id=p_tenant
  AND x.workflow_id=p_workflow AND x.project_id=w.project_id AND x.entry_id=p_entry
  AND x.target_version_id=p_target AND x.kind=p_kind AND x.proposal_id=p_proposal
  AND x.proposal_sha=p_proposal_sha AND x.base_id=p_base AND x.base_sha=p_base_sha
  AND x.label_map_id IS NOT DISTINCT FROM p_label AND x.label_map_sha IS NOT DISTINCT FROM p_label_sha
  AND x.status='granted' FOR SHARE;
 IF NOT FOUND OR d.epoch<1 THEN RETURN;END IF;
 RETURN QUERY SELECT d.decision,d.accepted_ids,d.rejected_ids,d.epoch;
END $$;
ALTER FUNCTION photo.v30_import_review_live_decision(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_review_live_decision(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_review_live_decision(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
