-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_run_current(p_tenant uuid,p_preview uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;config jsonb;e jsonb;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_preview;
 config:=r.config_snapshot->'semantic_preapply_preview';e:=config->'params';
 IF r.id IS NULL OR r.kind<>'preview' OR r.cancel_requested
 OR jsonb_typeof(config) IS DISTINCT FROM 'object' OR jsonb_typeof(e) IS DISTINCT FROM 'object'
 OR config->>'actor_id' IS DISTINCT FROM r.created_by::text THEN RETURN false;END IF;
 RETURN photo.semantic_preapply_preview_current(p_tenant,(e->>'intent_id')::uuid,(e->>'target_id')::uuid,
 (config->>'actor_id')::uuid,(config->>'session_id')::uuid,(e->>'workflow_revision')::bigint,
 (e->>'target_revision')::bigint,e->'plan'->>'sha256',e->'candidate'->>'sha256');
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_preapply_preview_run_current(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_run_current(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.cancel_stale_semantic_preapply_preview() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;e jsonb;parent uuid;has_quota boolean;has_reserved boolean;
BEGIN
 SELECT x.tenant_id,x.id INTO candidate FROM photo.runs x
 JOIN photo.tenants n ON n.id=x.tenant_id AND n.status='active'
 WHERE x.kind='preview' AND jsonb_typeof(x.config_snapshot->'semantic_preapply_preview')='object'
 AND x.status IN('draft','queued','running') AND NOT x.cancel_requested
 AND (NOT photo.semantic_preapply_preview_run_current(x.tenant_id,x.id)
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
 e:=r.config_snapshot->'semantic_preapply_preview'->'params';
 SELECT w.run_id INTO parent FROM photo.v2_workflows w WHERE w.tenant_id=r.tenant_id AND w.id=(e->>'workflow_id')::uuid;
 -- Publication lock order: tenant, parent, target, preview, usage/quota.
 PERFORM id FROM photo.runs WHERE tenant_id=r.tenant_id AND id=parent FOR UPDATE;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=r.tenant_id AND id=(e->>'target_id')::uuid FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.status NOT IN('draft','queued','running') THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=r.tenant_id FOR UPDATE;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=r.tenant_id AND holder_type='run' AND holder_id=r.id FOR UPDATE;
 has_reserved:=EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=r.tenant_id
 AND holder_type='run' AND holder_id=r.id AND state='reserved');
 has_quota:=EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=r.tenant_id
 AND holder_type='run' AND holder_id=r.id AND state='reserved' AND expires_at>clock_timestamp());
 IF photo.semantic_preapply_preview_run_current(r.tenant_id,r.id)
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
