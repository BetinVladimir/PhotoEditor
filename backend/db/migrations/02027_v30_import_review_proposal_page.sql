-- +goose Up
-- Keyset page of current prepared defect/dust proposals. Each row remains
-- display evidence; a grant still needs a fresh exact object read.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_review_proposal_page(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_parsed text,
 p_after_entry uuid,p_after_target uuid,p_after_kind text,p_after_proposal uuid)
RETURNS TABLE(entry_id uuid,target_version_id uuid,kind text,proposal_id uuid,
 proposal_sha text,base_id uuid,base_sha text,decision_status text,decision_epoch bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;sources jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_workflow IS NULL
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR ((p_after_entry IS NULL OR p_after_target IS NULL OR p_after_kind IS NULL OR p_after_proposal IS NULL)
     AND (p_after_entry IS NOT NULL OR p_after_target IS NOT NULL OR p_after_kind IS NOT NULL OR p_after_proposal IS NOT NULL))
 OR (p_after_kind IS NOT NULL AND p_after_kind NOT IN('defects','dust')) THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant AND x.id=p_workflow
  AND x.workflow_type='interchange' AND x.renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.id=w.run_id FOR SHARE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN;END IF;
 RETURN QUERY
 WITH adopted AS (
  SELECT e.value AS body FROM jsonb_array_elements(sources->'entries') e
  WHERE e.value->>'state'='adopted'
 ), proposals AS (
  SELECT (adopted.body->>'entry_id')::uuid AS entry_id,
   (adopted.body->>'asset_version_id')::uuid AS target_version_id,
   CASE b.v30_import_prepared->>'contract' WHEN 'DefectCandidates' THEN 'defects' ELSE 'dust' END AS kind,
   b.artifact_id AS proposal_id,b.sha256 AS proposal_sha,
   (adopted.body#>>'{master,artifact_id}')::uuid AS base_id,
   adopted.body#>>'{master,sha256}' AS base_sha
  FROM adopted
  JOIN photo.backend_documents b ON b.tenant_id=p_tenant AND b.run_id=w.run_id
   AND b.purpose='v30_import_prepared' AND b.state='ready'
   AND b.v30_import_prepared->>'contract' IN('DefectCandidates','DustCandidates')
   AND b.v30_import_prepared->>'workflow'=p_workflow::text
   AND b.v30_import_prepared->>'entry'=adopted.body->>'entry_id'
   AND b.v30_import_prepared#>>'{binding,asset_version_id}'=adopted.body->>'asset_version_id'
   AND b.v30_import_prepared#>'{binding,source}'=adopted.body->'master'
   AND b.v30_import_prepared->>'sha'=b.sha256
  JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
   AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes
   AND a.bucket=b.bucket AND a.object_version=b.object_version
   AND a.output_role='v30_import_prepared' AND a.media_type='application/json'
   AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND a.bytes BETWEEN 2 AND 1048576
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 )
 SELECT p.entry_id,p.target_version_id,p.kind,p.proposal_id,p.proposal_sha,p.base_id,p.base_sha,
  d.status,d.epoch FROM proposals p
 LEFT JOIN photo.v30_import_review_decisions d ON d.tenant_id=p_tenant AND d.workflow_id=p_workflow
  AND d.project_id=w.project_id AND d.entry_id=p.entry_id AND d.target_version_id=p.target_version_id
  AND d.kind=p.kind AND d.proposal_id=p.proposal_id AND d.proposal_sha=p.proposal_sha
  AND d.base_id=p.base_id AND d.base_sha=p.base_sha
 WHERE p_after_entry IS NULL OR
  (p.entry_id,p.target_version_id,p.kind,p.proposal_id)>(p_after_entry,p_after_target,p_after_kind,p_after_proposal)
 ORDER BY p.entry_id,p.target_version_id,p.kind,p.proposal_id LIMIT 51;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_import_review_proposal_page(uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_review_proposal_page(uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_review_proposal_page(uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
