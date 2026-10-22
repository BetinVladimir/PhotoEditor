-- +goose Up
-- Background cleanup lease admission, persisted closure and current review authority.
-- Producer/registry activation remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.admit_semantic_background_cleanup_preview(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;s photo.resource_slots%ROWTYPE;
 e jsonb; config jsonb;actor uuid;session_id uuid;identity record;backend text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_sha
 OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF r.id IS NULL OR r.kind<>'preview' OR r.status NOT IN('queued','running')
 OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF t.id IS NULL OR t.operation<>'semantic.preview_background_cleanup' OR t.operation_version<>'1'
 OR t.resource_class<>'gpu' OR t.state<>'running' OR t.cancel_requested
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.worker_id IS DISTINCT FROM p_worker OR t.params_artifact_id IS DISTINCT FROM p_params
 OR t.input_digest IS DISTINCT FROM p_digest OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR coalesce(t.execution_profile->>'backend','') NOT IN('cpu','cuda','mps')
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false;END IF;
 backend:=t.execution_profile->>'backend';
 SELECT * INTO s FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF s.id IS NULL OR s.resource_class IS DISTINCT FROM 'gpu' OR s.fencing_token IS DISTINCT FROM p_fence
 OR s.lease_until IS NULL OR s.lease_until<=clock_timestamp()
 OR photo.worker_authorized(p_tenant,p_worker,'gpu',s.node_id,t.operation,backend,t.execution_profile->>'model_lock_id') IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM worker_id FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker
 AND db_principal=session_user AND enabled AND v2_renderer_versions ? 'v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_sha
 AND state='ready' AND verified_at IS NOT NULL AND media_type='application/json'
 AND bytes=octet_length(p_body) AND object_version IS NOT NULL AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 e:=p_body::jsonb;config:=r.config_snapshot->'semantic_background_cleanup_preview';
 IF jsonb_typeof(e) IS DISTINCT FROM 'object' OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(config) IS DISTINCT FROM 'object' OR config->'params' IS DISTINCT FROM e
 OR config->>'params_sha' IS DISTINCT FROM p_sha OR e->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR e->>'snapshot_id' IS DISTINCT FROM r.input_snapshot_id::text THEN RETURN false;END IF;
 IF e->>'kind' IS DISTINCT FROM 'background_cleanup' OR e->>'model_id' IS DISTINCT FROM 'big_lama'
 OR jsonb_typeof(e->'context_pixels') IS DISTINCT FROM 'number'
 OR jsonb_typeof(e->'feather_pixels') IS DISTINCT FROM 'number'
 OR (e->>'context_pixels')::integer NOT BETWEEN 32 AND 512
 OR (e->>'feather_pixels')::integer NOT BETWEEN 1 AND 64
 OR e->'context_pixels' IS DISTINCT FROM to_jsonb((e->>'context_pixels')::integer)
 OR e->'feather_pixels' IS DISTINCT FROM to_jsonb((e->>'feather_pixels')::integer)
 THEN RETURN false;END IF;
 actor:=(config->>'actor_id')::uuid;session_id:=(config->>'session_id')::uuid;
 IF actor IS NULL OR session_id IS NULL OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
 SELECT * INTO identity FROM photo.semantic_review_identity(p_tenant,(e->>'intent_id')::uuid,actor,session_id);
 IF identity.workflow_id::text IS DISTINCT FROM e->>'workflow_id'
 OR photo.semantic_preapply_preview_current(p_tenant,(e->>'intent_id')::uuid,(e->>'target_id')::uuid,
  actor,session_id,(e->>'workflow_revision')::bigint,(e->>'target_revision')::bigint,
  e->'plan'->>'sha256',e->'candidate'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF photo.v27_downstream_qualification_pin_ready(p_tenant,identity.project_id,
 'semantic.preview_background_cleanup','1',backend,t.execution_profile->'qualification') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF e->>'model_lock_sha' IS DISTINCT FROM t.execution_profile->'qualification'->>'model_lock_sha'
 OR e->>'runtime_lock_sha' IS DISTINCT FROM t.execution_profile->'qualification'->>'runtime_lock_sha'
 OR e->>'device_profile_sha' IS DISTINCT FROM t.execution_profile->'qualification'->>'device_profile_sha' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_review_plan_sources(p_tenant,identity.workflow_id,actor,session_id) source
  WHERE source.contract='SemanticPlan' AND source.artifact_id::text=e->'plan'->>'artifact_id'
  AND source.sha256=e->'plan'->>'sha256') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_targets target JOIN photo.semantic_intents intent
  ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id
  WHERE target.tenant_id=p_tenant AND target.id=(e->>'target_id')::uuid
  AND target.intent_id=(e->>'intent_id')::uuid
  AND target.asset_version_id::text=e->'source'->>'asset_version_id'
  AND target.candidate_artifact_id::text=e->'candidate'->>'artifact_id'
  AND intent.snapshot_id=r.input_snapshot_id AND intent.intent_artifact_id::text=e->'intent'->>'artifact_id'
  AND intent.intent_sha=e->'intent'->>'sha256') THEN RETURN false;END IF;
 IF photo.semantic_background_cleanup_preview_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp() AND s.lease_until>clock_timestamp();
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.admit_semantic_background_cleanup_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_semantic_background_cleanup_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.admit_semantic_background_cleanup_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
