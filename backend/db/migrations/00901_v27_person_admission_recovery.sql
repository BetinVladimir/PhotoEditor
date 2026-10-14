-- +goose Up
-- Person checkpoints reuse guarded per-run recovery lease columns, in an independent queue.
-- Enable inside original blocked-draft transaction, before transport publication.
-- +goose StatementBegin
CREATE FUNCTION photo.enable_v27_person_admission(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_person uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_person IS DISTINCT FROM photo.semantic_uuid_v5(p_parent,'semantic.mask.person:reference') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,p_person,'mask.person')) THEN RETURN false;END IF;
 UPDATE photo.runs SET v27_initial_recovery_enabled=true WHERE tenant_id=p_tenant AND id=p_person;
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.enable_v27_person_admission(uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enable_v27_person_admission(uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.enable_v27_person_admission(uuid,uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.claim_v27_person_admission(p_project uuid,p_seconds integer)
RETURNS TABLE(tenant_id uuid,person_run_id uuid,parent_run_id uuid,actor_id uuid,session_id uuid,auth_epoch bigint,lease_token uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;token uuid:=gen_random_uuid();
BEGIN
 IF p_project IS NULL OR p_seconds IS NULL OR p_seconds NOT BETWEEN 11 AND 1800 THEN RETURN;END IF;
 FOR candidate IN
 SELECT r.id detector,r.tenant_id tenant,ids.parent,r.created_by actor,s.id session,u.auth_epoch epoch
 FROM photo.runs r
 JOIN photo.tenants tt ON tt.id=r.tenant_id AND tt.status='active'
 JOIN photo.projects p ON p.tenant_id=r.tenant_id AND p.id=p_project AND p.id::text=r.config_snapshot->>'project_id' AND p.archived_at IS NULL
 JOIN photo.users u ON u.id=r.created_by
 JOIN photo.sessions s ON s.user_id=u.id AND s.id::text=r.config_snapshot->'v27_person_prepare'->>'auth_session_id' AND s.revoked_at IS NULL
 CROSS JOIN LATERAL (SELECT CASE WHEN r.config_snapshot->'v27_person_prepare'->>'parent_run_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (r.config_snapshot->'v27_person_prepare'->>'parent_run_id')::uuid END parent) ids
 WHERE r.kind='v2' AND NOT r.cancel_requested AND r.v27_initial_recovery_enabled AND NOT r.v27_initial_recovery_done
 AND r.id=photo.semantic_uuid_v5(ids.parent,'semantic.mask.person:reference')
 AND r.config_snapshot->'v27_person_prepare'->'schema_version'='1'::jsonb
 AND jsonb_typeof(r.config_snapshot->'v27_person_prepare'->'recovery'->'request_key')='string'
 AND length(r.config_snapshot->'v27_person_prepare'->'recovery'->>'request_key') BETWEEN 1 AND 128
 AND (r.v27_initial_recovery_after IS NULL OR r.v27_initial_recovery_after<=clock_timestamp())
 AND (r.v27_initial_recovery_until IS NULL OR r.v27_initial_recovery_until<=clock_timestamp())
 AND photo.v2_editor_authorized(r.tenant_id,p.id,r.created_by,s.id)
 ORDER BY r.v27_initial_recovery_after NULLS FIRST,r.created_at,r.id
 FOR UPDATE OF r SKIP LOCKED LIMIT 1
 LOOP
 UPDATE photo.runs SET v27_initial_recovery_token=token,v27_initial_recovery_until=clock_timestamp()+make_interval(secs=>p_seconds) WHERE id=candidate.detector AND photo.runs.tenant_id=candidate.tenant;
 RETURN QUERY SELECT candidate.tenant,candidate.detector,candidate.parent,candidate.actor,candidate.session,candidate.epoch,token;
 END LOOP;
END $$;
ALTER FUNCTION photo.claim_v27_person_admission(uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v27_person_admission(uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v27_person_admission(uuid,integer) TO photo_scheduler;

CREATE FUNCTION photo.next_v27_person_admission_project(p_after uuid) RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT DISTINCT p.id FROM photo.runs r
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.projects p ON p.tenant_id=r.tenant_id AND p.id::text=r.config_snapshot->>'project_id' AND p.archived_at IS NULL
 JOIN photo.users u ON u.id=r.created_by
 JOIN photo.sessions s ON s.user_id=u.id AND s.id::text=r.config_snapshot->'v27_person_prepare'->>'auth_session_id' AND s.revoked_at IS NULL
 WHERE r.kind='v2' AND r.v27_initial_recovery_enabled AND NOT r.v27_initial_recovery_done AND NOT r.cancel_requested
 AND jsonb_typeof(r.config_snapshot->'v27_person_prepare'->'recovery'->'request_key')='string'
 AND length(r.config_snapshot->'v27_person_prepare'->'recovery'->>'request_key') BETWEEN 1 AND 128
 AND (p_after IS NULL OR p.id>p_after)
 AND (r.v27_initial_recovery_after IS NULL OR r.v27_initial_recovery_after<=clock_timestamp())
 AND (r.v27_initial_recovery_until IS NULL OR r.v27_initial_recovery_until<=clock_timestamp())
 AND photo.v2_editor_authorized(r.tenant_id,p.id,r.created_by,s.id)
 ORDER BY p.id LIMIT 1;
$$;
ALTER FUNCTION photo.next_v27_person_admission_project(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v27_person_admission_project(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.next_v27_person_admission_project(uuid) TO photo_scheduler;
CREATE FUNCTION photo.record_v27_person_admission(p_tenant uuid,p_person uuid,p_token uuid,p_outcome text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;
BEGIN
 IF p_token IS NULL OR p_outcome IS NULL OR p_outcome NOT IN('retry','waiting','admitted') THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_person FOR UPDATE;
 IF NOT FOUND OR NOT r.v27_initial_recovery_enabled OR r.v27_initial_recovery_done
 OR r.v27_initial_recovery_token IS DISTINCT FROM p_token OR r.v27_initial_recovery_until IS NULL OR r.v27_initial_recovery_until<=clock_timestamp()
 OR jsonb_typeof(r.config_snapshot->'v27_person_prepare') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF p_outcome='admitted' AND (NOT r.graph_sealed OR NOT EXISTS(
 SELECT 1 FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant AND i.resource_id=p_person AND i.route='V2.Admit' AND i.response_status=202)) THEN RETURN false;END IF;
 UPDATE photo.runs SET v27_initial_recovery_token=NULL,v27_initial_recovery_until=NULL,v27_initial_recovery_done=(p_outcome='admitted'),
 v27_initial_recovery_failures=CASE WHEN p_outcome='retry' THEN least(r.v27_initial_recovery_failures::bigint+1,2147483647)::integer ELSE 0 END,
 v27_initial_recovery_after=CASE WHEN p_outcome='admitted' THEN NULL WHEN p_outcome='retry' THEN clock_timestamp()+make_interval(secs=>least(300,5*power(2,least(r.v27_initial_recovery_failures,6)))::integer) ELSE clock_timestamp()+interval '5 seconds' END
 WHERE tenant_id=p_tenant AND id=p_person;
 RETURN true;
END $$;
ALTER FUNCTION photo.record_v27_person_admission(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v27_person_admission(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v27_person_admission(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
