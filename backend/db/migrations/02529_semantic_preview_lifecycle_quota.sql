-- +goose Up
-- Fix semantic preview ownership: these runs do not use v26_derivative_task_id.
-- Preserve terminal/task/upload/document and reserved-usage gates.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.release_semantic_preapply_preview_quota(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT ((SELECT count(*) FROM unnest(ARRAY['semantic_preapply_preview','semantic_removal_preview','semantic_background_cleanup_preview','semantic_retouch_preview']) key WHERE jsonb_typeof(r.config_snapshot->key)='object')=1) OR r.v27_preapply_preview_profile IS NULL OR r.kind<>'preview'
  OR r.status NOT IN('completed','completed_with_errors','failed','cancelled') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 OR EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND u.state NOT IN('ready','cleaned'))
 OR EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.state NOT IN('ready','cleaned')) THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN true;END IF;
 IF q.state='released' AND q.bytes=0 THEN RETURN true;END IF;
 IF q.state<>'reserved' OR q.principal_type<>'user' OR q.principal_id IS DISTINCT FROM r.created_by OR q.share_id IS NOT NULL OR q.files<>0 THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_PREVIEW_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
 UPDATE photo.quota_reservations SET state='released',bytes=0,expires_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=q.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_semantic_preapply_preview_quota(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_semantic_preapply_preview_quota(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.release_semantic_preapply_preview_quota(uuid,uuid) TO photo_scheduler;
-- One settled terminal reservation per pass; unfinished uploads do not starve later work.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.release_next_semantic_preapply_preview_quota() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved'
 AND q.principal_type='user' AND q.principal_id=r.created_by AND q.share_id IS NULL AND q.files=0
 WHERE ((SELECT count(*) FROM unnest(ARRAY['semantic_preapply_preview','semantic_removal_preview','semantic_background_cleanup_preview','semantic_retouch_preview']) key WHERE jsonb_typeof(r.config_snapshot->key)='object')=1) AND r.v27_preapply_preview_profile IS NOT NULL AND r.kind='preview'
 AND r.status IN('completed','completed_with_errors','failed','cancelled')
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 AND NOT EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND u.state NOT IN('ready','cleaned'))
 AND NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.state NOT IN('ready','cleaned'))
 ORDER BY r.created_at,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN photo.release_semantic_preapply_preview_quota(candidate.tenant_id,candidate.id);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_next_semantic_preapply_preview_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_next_semantic_preapply_preview_quota() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.release_next_semantic_preapply_preview_quota() TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preview_lifecycle_current(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;k text;keys text[];
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT array_agg(key) INTO keys FROM unnest(ARRAY['semantic_preapply_preview','semantic_removal_preview','semantic_background_cleanup_preview','semantic_retouch_preview']) key WHERE jsonb_typeof(r.config_snapshot->key) IS NOT DISTINCT FROM 'object';
 IF coalesce(cardinality(keys),0)<>1 THEN RETURN false;END IF;
 k:=keys[1];
 CASE k
 WHEN 'semantic_preapply_preview' THEN RETURN photo.semantic_preapply_preview_run_current(p_tenant,p_run);
 WHEN 'semantic_removal_preview' THEN RETURN photo.semantic_removal_preview_run_current(p_tenant,p_run);
 WHEN 'semantic_background_cleanup_preview' THEN RETURN photo.semantic_background_cleanup_preview_run_current(p_tenant,p_run);
 WHEN 'semantic_retouch_preview' THEN RETURN photo.semantic_retouch_preview_run_current(p_tenant,p_run);
 ELSE RETURN false;
 END CASE;
END $$;
ALTER FUNCTION photo.semantic_preview_lifecycle_current(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preview_lifecycle_current(uuid,uuid) FROM PUBLIC,photo_api,photo_auth,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance,photo_publisher,photo_mail,photo_semantic_admission;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.cancel_stale_semantic_preapply_preview() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;e jsonb;parent uuid;has_quota boolean;has_reserved boolean;k text;
BEGIN
 SELECT x.tenant_id,x.id INTO candidate FROM photo.runs x
 JOIN photo.tenants n ON n.id=x.tenant_id AND n.status='active'
 WHERE x.kind='preview' AND (jsonb_typeof(x.config_snapshot->'semantic_preapply_preview') IS NOT DISTINCT FROM 'object' OR jsonb_typeof(x.config_snapshot->'semantic_removal_preview') IS NOT DISTINCT FROM 'object' OR jsonb_typeof(x.config_snapshot->'semantic_background_cleanup_preview') IS NOT DISTINCT FROM 'object' OR jsonb_typeof(x.config_snapshot->'semantic_retouch_preview') IS NOT DISTINCT FROM 'object')
 AND x.status IN('draft','queued','running') AND NOT x.cancel_requested
 AND (NOT photo.semantic_preview_lifecycle_current(x.tenant_id,x.id)
 OR EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=x.tenant_id
  AND q.holder_type='run' AND q.holder_id=x.id AND q.state='reserved' AND q.expires_at<=clock_timestamp())
 OR (x.created_at<=clock_timestamp()-interval '1 hour' AND NOT EXISTS(
  SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=x.tenant_id
  AND q.holder_type='run' AND q.holder_id=x.id AND q.state='reserved')))
 ORDER BY x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id;
 SELECT key INTO k FROM unnest(ARRAY['semantic_preapply_preview','semantic_removal_preview','semantic_background_cleanup_preview','semantic_retouch_preview']) key WHERE jsonb_typeof(r.config_snapshot->key) IS NOT DISTINCT FROM 'object' LIMIT 1;
 e:=r.config_snapshot->k->'params';
 SELECT w.run_id INTO parent FROM photo.v2_workflows w WHERE w.tenant_id=r.tenant_id AND w.id=(e->>'workflow_id')::uuid;
 -- Publication lock order: tenant, parent, target, preview, usage/quota.
 PERFORM id FROM photo.runs WHERE tenant_id=r.tenant_id AND id=parent FOR UPDATE;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=r.tenant_id AND id=(e->>'target_id')::uuid FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.status NOT IN('draft','queued','running') OR r.revision>=9007199254740991 THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=r.tenant_id FOR UPDATE;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=r.tenant_id AND holder_type='run' AND holder_id=r.id FOR UPDATE;
 has_reserved:=EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=r.tenant_id
 AND holder_type='run' AND holder_id=r.id AND state='reserved');
 has_quota:=EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=r.tenant_id
 AND holder_type='run' AND holder_id=r.id AND state='reserved' AND expires_at>clock_timestamp());
 IF photo.semantic_preview_lifecycle_current(r.tenant_id,r.id)
 AND (has_quota OR (NOT has_reserved AND r.created_at>clock_timestamp()-interval '1 hour')) THEN RETURN false;END IF;
 UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id;
 UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=r.tenant_id AND run_id=r.id
 AND state NOT IN('succeeded','failed','cancelled','skipped');
 RETURN true;
END $$;
ALTER FUNCTION photo.cancel_stale_semantic_preapply_preview() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.cancel_stale_semantic_preapply_preview() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.cancel_stale_semantic_preapply_preview() TO photo_scheduler;
-- +goose StatementEnd

-- Execution finalization only: no photographic acceptance, Apply or early quota release.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_next_semantic_retouch_preview() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;e jsonb;parent uuid;done text;
BEGIN
 SELECT x.tenant_id,x.id INTO candidate FROM photo.runs x
 JOIN photo.tenants tenant ON tenant.id=x.tenant_id AND tenant.status='active'
 JOIN photo.tasks task ON task.tenant_id=x.tenant_id AND task.run_id=x.id
 WHERE x.kind='preview' AND x.status IN('queued','running') AND x.graph_sealed AND NOT x.cancel_requested
 AND x.v27_preapply_preview_profile IS NOT NULL
 AND jsonb_typeof(x.config_snapshot->'semantic_retouch_preview') IS NOT DISTINCT FROM 'object'
 AND task.operation='semantic.preview_retouch_region' AND task.operation_version='1'
 AND task.state IN('succeeded','failed','cancelled','skipped')
 AND task.resource_class='cpu'
 AND task.id=photo.semantic_uuid_v5(x.id,'semantic.preview_retouch_region:'||
  (x.config_snapshot->'semantic_retouch_preview'->'params'->>'target_id')::uuid::text)
 AND NOT EXISTS(SELECT 1 FROM photo.tasks extra WHERE extra.tenant_id=x.tenant_id AND extra.run_id=x.id AND extra.id<>task.id)
 -- Filter ineligible successes before LIMIT: one malformed/expired result must
 -- not starve every later terminal run. Authority is repeated under locks below.
 AND (task.state<>'succeeded' OR (NOT task.cancel_requested
  AND photo.semantic_retouch_preview_run_current(x.tenant_id,x.id) IS TRUE
  AND photo.semantic_retouch_preview_closure_ready(task.id) IS TRUE
  AND photo.semantic_retouch_preview_bundle_ready(task.id,task.canonical_result_id,task.attempt,task.fencing_token) IS TRUE
  AND EXISTS(SELECT 1 FROM photo.task_results z JOIN photo.artifacts a
   ON a.tenant_id=z.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid AND a.sha256=z.outputs->0->>'sha256'
   WHERE z.tenant_id=task.tenant_id AND z.task_id=task.id AND z.id=task.canonical_result_id
   AND z.attempt=task.attempt AND z.fencing_token=task.fencing_token
   AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
   AND a.created_by_task_id=task.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
   AND a.output_role='preview_presentation' AND a.media_type='application/json'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 1048576
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))))
 ORDER BY x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id;
 e:=r.config_snapshot->'semantic_retouch_preview'->'params';
 SELECT w.run_id INTO parent FROM photo.v2_workflows w WHERE w.tenant_id=r.tenant_id AND w.id=(e->>'workflow_id')::uuid;
 PERFORM id FROM photo.runs WHERE tenant_id=r.tenant_id AND id=parent FOR UPDATE;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=r.tenant_id AND id=(e->>'target_id')::uuid FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'preview' OR r.cancel_requested OR NOT r.graph_sealed
 OR r.status NOT IN('queued','running') OR r.revision>=9007199254740991
 OR r.v27_preapply_preview_profile IS NULL THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id
 AND id=photo.semantic_uuid_v5(r.id,'semantic.preview_retouch_region:'||(e->>'target_id')::uuid::text) FOR UPDATE;
 IF NOT FOUND OR t.operation<>'semantic.preview_retouch_region' OR t.operation_version<>'1'
 OR t.resource_class<>'cpu' OR t.state NOT IN('succeeded','failed','cancelled','skipped')
 OR EXISTS(SELECT 1 FROM photo.tasks extra WHERE extra.tenant_id=r.tenant_id AND extra.run_id=r.id AND extra.id<>t.id)
 THEN RETURN false;END IF;
 done:='failed';
 IF t.state='succeeded' THEN
  IF t.cancel_requested OR photo.semantic_retouch_preview_run_current(r.tenant_id,r.id) IS DISTINCT FROM true
  OR photo.semantic_retouch_preview_closure_ready(t.id) IS DISTINCT FROM true
  OR photo.semantic_retouch_preview_bundle_ready(t.id,t.canonical_result_id,t.attempt,t.fencing_token) IS DISTINCT FROM true
  OR NOT EXISTS(SELECT 1 FROM photo.task_results z JOIN photo.artifacts a
   ON a.tenant_id=z.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid AND a.sha256=z.outputs->0->>'sha256'
   WHERE z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
   AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
   AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
   AND a.output_role='preview_presentation' AND a.media_type='application/json'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 1048576
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
  done:='completed';
 END IF;
 UPDATE photo.runs SET status=done,revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.finalize_next_semantic_retouch_preview() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_next_semantic_retouch_preview() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.finalize_next_semantic_retouch_preview() TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER FUNCTION photo.finalize_next_semantic_preapply_preview() RENAME TO finalize_next_semantic_preapply_preview_before_retouch;
REVOKE ALL ON FUNCTION photo.finalize_next_semantic_preapply_preview_before_retouch() FROM PUBLIC,photo_api,photo_auth,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance,photo_publisher,photo_mail,photo_semantic_admission;
CREATE FUNCTION photo.finalize_next_semantic_preapply_preview() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.finalize_next_semantic_preapply_preview_before_retouch() THEN RETURN true;END IF;
 RETURN photo.finalize_next_semantic_retouch_preview();
END $$;
ALTER FUNCTION photo.finalize_next_semantic_preapply_preview() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_next_semantic_preapply_preview() FROM PUBLIC,photo_api,photo_auth,photo_worker,photo_portal,photo_gateway,photo_maintenance,photo_publisher,photo_mail,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.finalize_next_semantic_preapply_preview() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
