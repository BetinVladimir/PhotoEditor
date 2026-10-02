-- +goose Up
ALTER TABLE photo.reviews ADD COLUMN resolved_artifact_id uuid;
ALTER TABLE photo.reviews ADD COLUMN proposal_report_artifact_id uuid;
ALTER TABLE photo.reviews ADD CONSTRAINT defect_context_pair CHECK((resolved_artifact_id IS NULL)=(proposal_report_artifact_id IS NULL));
ALTER TABLE photo.reviews ADD FOREIGN KEY(tenant_id,resolved_artifact_id) REFERENCES photo.artifacts(tenant_id,id);
ALTER TABLE photo.reviews ADD FOREIGN KEY(tenant_id,proposal_report_artifact_id) REFERENCES photo.artifacts(tenant_id,id);
DROP TRIGGER immutable_content ON photo.reviews;
CREATE TRIGGER immutable_content BEFORE UPDATE ON photo.reviews FOR EACH ROW
 EXECUTE FUNCTION photo.reject_content_update('tenant_id','id','created_at','run_id','task_id','candidate_artifact_id','base_artifact_id','candidate_hash','kind','reasons','resolved_artifact_id','proposal_report_artifact_id');

-- +goose StatementBegin
CREATE FUNCTION photo.defect_expansion_context(p_expansion uuid,p_token uuid,p_decision_hash text)
RETURNS TABLE(resolved_id uuid,resolved_sha text,report_id uuid,report_sha text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT style.id,style.sha256,report.id,report.sha256
 FROM photo.graph_expansions e JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 JOIN photo.reviews v ON v.tenant_id=e.tenant_id AND v.run_id=e.run_id AND v.task_id=e.parent_task_id AND e.kind='review.defects/'||v.id::text
 JOIN photo.artifacts style ON style.tenant_id=v.tenant_id AND style.id=v.resolved_artifact_id AND style.state='ready' AND style.output_role='resolved_style'
 JOIN photo.tasks st ON st.tenant_id=style.tenant_id AND st.id=style.created_by_task_id AND st.run_id=e.run_id AND st.operation='style.resolve' AND st.state='succeeded'
 JOIN photo.task_results sr ON sr.tenant_id=st.tenant_id AND sr.id=st.canonical_result_id AND sr.task_id=st.id
 JOIN photo.artifacts report ON report.tenant_id=v.tenant_id AND report.id=v.proposal_report_artifact_id AND report.state='ready' AND report.output_role='defect_propose_report'
 JOIN photo.tasks pt ON pt.tenant_id=report.tenant_id AND pt.id=report.created_by_task_id AND pt.id=e.parent_task_id AND pt.run_id=e.run_id AND pt.operation='defects.propose' AND pt.state='succeeded'
 JOIN photo.task_results pr ON pr.tenant_id=pt.tenant_id AND pr.id=pt.canonical_result_id AND pr.task_id=pt.id
 WHERE e.id=p_expansion AND e.state='open' AND e.expansion_token=p_token AND e.expansion_lease_until>clock_timestamp()
 AND NOT r.cancel_requested AND NOT r.graph_sealed AND r.status IN ('running','waiting_review')
 AND v.kind='defects' AND v.state='decided' AND v.decision_hash=p_decision_hash
 AND sr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',style.id::text,'sha256',style.sha256))
 AND pr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',report.id::text,'sha256',report.sha256));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.defect_expansion_context(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.defect_expansion_context(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.defect_expansion_context(uuid,uuid,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.expansion_artifact(p_expansion uuid,p_token uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,output_role text,producer_operation text,is_canonical boolean)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,a.output_role,t.operation,
 coalesce(t.state='succeeded' AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256)),false)
 FROM photo.graph_expansions e JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 JOIN photo.artifacts a ON a.tenant_id=e.tenant_id AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready'
 LEFT JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 LEFT JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 WHERE e.id=p_expansion AND e.kind LIKE 'review.defects/%' AND e.state='open'
 AND e.expansion_token=p_token AND e.expansion_lease_until>clock_timestamp()
 AND NOT r.cancel_requested AND NOT r.graph_sealed AND r.status IN ('running','waiting_review');
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.expansion_artifact(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.expansion_artifact(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.expansion_artifact(uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
