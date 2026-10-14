-- +goose Up
-- Private relational predicate. Worker entrypoints must additionally enforce
-- authenticated worker identity, task lease/fence/resources and exact edit bytes.
-- Parent completion/cancellation does not revoke already admitted children.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_child_source(p_tenant uuid,p_child uuid,p_target uuid,p_version uuid,p_candidate uuid,p_candidate_sha text,p_source uuid,p_source_sha text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
 SELECT 1 FROM photo.semantic_targets t
 JOIN photo.semantic_intents i ON i.tenant_id=t.tenant_id AND i.project_id=t.project_id AND i.id=t.intent_id
 JOIN photo.v2_review_batches b ON b.tenant_id=t.tenant_id AND b.project_id=t.project_id AND b.id=t.batch_id
 AND b.workflow_id=i.workflow_id AND b.action='apply' AND b.child_run_id=t.child_run_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.child_run_id AND r.kind='apply'
 AND r.config_snapshot->>'project_id'=t.project_id::text AND r.created_by=b.actor_id
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.snapshot_items si ON si.tenant_id=r.tenant_id AND si.snapshot_id=r.input_snapshot_id
 AND si.asset_version_id=t.asset_version_id AND si.artifact_id=p_source AND si.sha256=p_source_sha
 JOIN photo.asset_versions v ON v.tenant_id=t.tenant_id AND v.id=t.asset_version_id
 AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id AND asset.project_id=t.project_id
 AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts source ON source.tenant_id=t.tenant_id AND source.id=p_source AND source.sha256=p_source_sha
 AND source.state='ready' AND source.verified_at IS NOT NULL AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 JOIN photo.artifacts candidate ON candidate.tenant_id=t.tenant_id AND candidate.id=t.candidate_artifact_id AND candidate.sha256=p_candidate_sha
 AND candidate.state='ready' AND candidate.verified_at IS NOT NULL AND candidate.object_version IS NOT NULL AND candidate.object_version NOT IN('','null')
 AND (candidate.expires_at IS NULL OR candidate.expires_at>clock_timestamp())
 WHERE t.tenant_id=p_tenant AND t.id=p_target AND t.asset_version_id=p_version AND t.candidate_artifact_id=p_candidate
 AND t.state='queued' AND t.child_run_id=p_child AND r.status IN('queued','running') AND NOT r.cancel_requested
 );
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.semantic_approved_child_source(uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_child_source(uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
