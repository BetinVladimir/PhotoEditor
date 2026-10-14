-- +goose Up
-- Initial producer scheduling is separate from admitted manual-parent recovery.
-- Metadata lives on the original detector run; no new tenant table.
ALTER TABLE photo.runs ADD COLUMN v27_initial_recovery_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE photo.runs ADD COLUMN v27_initial_recovery_token uuid;
ALTER TABLE photo.runs ADD COLUMN v27_initial_recovery_until timestamptz;
ALTER TABLE photo.runs ADD COLUMN v27_initial_recovery_after timestamptz;
ALTER TABLE photo.runs ADD COLUMN v27_initial_recovery_failures integer NOT NULL DEFAULT 0 CHECK(v27_initial_recovery_failures>=0);
ALTER TABLE photo.runs ADD COLUMN v27_initial_recovery_done boolean NOT NULL DEFAULT false;
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v27_initial_recovery() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF current_user<>'photo_fn_owner' AND (
 (TG_OP='INSERT' AND (NEW.v27_initial_recovery_enabled OR NEW.v27_initial_recovery_token IS NOT NULL OR NEW.v27_initial_recovery_until IS NOT NULL OR NEW.v27_initial_recovery_after IS NOT NULL OR NEW.v27_initial_recovery_failures<>0 OR NEW.v27_initial_recovery_done))
 OR (TG_OP='UPDATE' AND (NEW.v27_initial_recovery_enabled IS DISTINCT FROM OLD.v27_initial_recovery_enabled OR NEW.v27_initial_recovery_token IS DISTINCT FROM OLD.v27_initial_recovery_token OR NEW.v27_initial_recovery_until IS DISTINCT FROM OLD.v27_initial_recovery_until OR NEW.v27_initial_recovery_after IS DISTINCT FROM OLD.v27_initial_recovery_after OR NEW.v27_initial_recovery_failures IS DISTINCT FROM OLD.v27_initial_recovery_failures OR NEW.v27_initial_recovery_done IS DISTINCT FROM OLD.v27_initial_recovery_done))) THEN
 RAISE EXCEPTION 'V27_INITIAL_RECOVERY_PRIVATE' USING ERRCODE='42501';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER v27_initial_recovery_private BEFORE INSERT OR UPDATE OF v27_initial_recovery_enabled,v27_initial_recovery_token,v27_initial_recovery_until,v27_initial_recovery_after,v27_initial_recovery_failures,v27_initial_recovery_done ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_initial_recovery();
REVOKE ALL ON FUNCTION photo.guard_v27_initial_recovery() FROM PUBLIC;

CREATE FUNCTION photo.seal_and_enable_v27_native_initial_pipeline(p_tenant uuid,p_detector uuid,p_packet jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ticket jsonb;
BEGIN
 IF p_packet->>'operation' IS DISTINCT FROM 'face.detect' THEN RETURN NULL;END IF;
 ticket:=photo.seal_semantic_native_preparation(p_tenant,p_detector,p_packet);
 IF ticket IS NULL THEN RETURN NULL;END IF;
 UPDATE photo.runs r SET v27_initial_recovery_enabled=true
 WHERE r.tenant_id=p_tenant AND r.id=p_detector AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
 AND r.config_snapshot->'v27_detector_prepare'->'schema_version'='1'::jsonb
 AND jsonb_typeof(r.config_snapshot->'v27_detector_prepare'->'recovery'->'request_key')='string'
 AND length(r.config_snapshot->'v27_detector_prepare'->'recovery'->>'request_key') BETWEEN 1 AND 128;
 IF NOT FOUND THEN RAISE EXCEPTION 'V27_INITIAL_ENABLE_FAILED' USING ERRCODE='23514';END IF;
 RETURN ticket;
END $$;
ALTER FUNCTION photo.seal_and_enable_v27_native_initial_pipeline(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_and_enable_v27_native_initial_pipeline(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_and_enable_v27_native_initial_pipeline(uuid,uuid,jsonb) TO photo_scheduler;

CREATE FUNCTION photo.claim_v27_native_initial_pipeline(p_project uuid,p_seconds integer)
RETURNS TABLE(tenant_id uuid,detector_run_id uuid,parent_run_id uuid,actor_id uuid,session_id uuid,auth_epoch bigint,lease_token uuid)
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
 JOIN photo.sessions s ON s.user_id=u.id AND s.id::text=r.config_snapshot->'v27_detector_prepare'->>'auth_session_id' AND s.revoked_at IS NULL
 CROSS JOIN LATERAL (SELECT CASE WHEN r.config_snapshot->'v27_detector_prepare'->>'parent_run_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (r.config_snapshot->'v27_detector_prepare'->>'parent_run_id')::uuid END parent) ids
 WHERE r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested AND r.v27_initial_recovery_enabled AND NOT r.v27_initial_recovery_done
 AND r.id=photo.semantic_uuid_v5(ids.parent,'semantic.face.detect:reference')
 AND r.config_snapshot->'v27_detector_prepare'->'schema_version'='1'::jsonb
 AND jsonb_typeof(r.config_snapshot->'v27_detector_prepare'->'recovery'->'request_key')='string'
 AND length(r.config_snapshot->'v27_detector_prepare'->'recovery'->>'request_key') BETWEEN 1 AND 128
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
ALTER FUNCTION photo.claim_v27_native_initial_pipeline(uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v27_native_initial_pipeline(uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v27_native_initial_pipeline(uuid,integer) TO photo_scheduler;

CREATE FUNCTION photo.record_v27_native_initial_pipeline(p_tenant uuid,p_detector uuid,p_token uuid,p_outcome text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;parent uuid;terminal boolean:=false;
BEGIN
 IF p_token IS NULL OR p_outcome IS NULL OR p_outcome NOT IN('retry','waiting','completed','failed') THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector FOR UPDATE;
 IF NOT FOUND OR r.v27_initial_recovery_token IS DISTINCT FROM p_token OR r.v27_initial_recovery_until<=clock_timestamp() OR r.v27_initial_recovery_until IS NULL THEN RETURN false;END IF;
 parent:=(r.config_snapshot->'v27_detector_prepare'->>'parent_run_id')::uuid;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF p_outcome='completed' THEN
 SELECT count(*)=4 AND coalesce(bool_and(t.state='succeeded' AND stage.graph_sealed),false) INTO terminal
 FROM (VALUES('face.detect'),('face.landmarks'),('face.parse'),('mask.face_skin')) operations(operation)
 CROSS JOIN LATERAL photo.semantic_native_input_saved_stage(p_tenant,(r.config_snapshot->>'project_id')::uuid,r.created_by,
  (r.config_snapshot->'v27_detector_prepare'->>'auth_session_id')::uuid,parent,
  photo.semantic_uuid_v5(parent,'semantic.'||operations.operation||':reference'),operations.operation) saved
 JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=saved.task_id
 JOIN photo.runs stage ON stage.tenant_id=t.tenant_id AND stage.id=t.run_id;
 IF NOT terminal THEN RETURN false;END IF;
 ELSIF p_outcome='failed' THEN
 SELECT EXISTS(SELECT 1 FROM photo.runs stage JOIN photo.tasks t ON t.tenant_id=stage.tenant_id AND t.run_id=stage.id
 WHERE stage.tenant_id=p_tenant AND stage.created_by=r.created_by AND stage.kind='v2'
 AND stage.config_snapshot->>'project_id'=r.config_snapshot->>'project_id'
 AND t.operation IN('face.detect','face.landmarks','face.parse','mask.face_skin') AND t.operation_version='1'
 AND (SELECT count(*) FROM photo.tasks other WHERE other.tenant_id=stage.tenant_id AND other.run_id=stage.id)=1
 AND stage.id=photo.semantic_uuid_v5(parent,'semantic.'||t.operation||':reference')
 AND stage.config_snapshot->CASE t.operation WHEN 'face.detect' THEN 'v27_detector_prepare' WHEN 'mask.face_skin' THEN 'v27_skin_prepare' ELSE 'v27_face_prepare' END->>'parent_run_id'=parent::text
 AND stage.config_snapshot->CASE t.operation WHEN 'face.detect' THEN 'v27_detector_prepare' WHEN 'mask.face_skin' THEN 'v27_skin_prepare' ELSE 'v27_face_prepare' END->>'auth_session_id'=r.config_snapshot->'v27_detector_prepare'->>'auth_session_id'
 AND stage.config_snapshot->CASE t.operation WHEN 'face.detect' THEN 'v27_detector_prepare' WHEN 'mask.face_skin' THEN 'v27_skin_prepare' ELSE 'v27_face_prepare' END->'request_sha'=r.config_snapshot->'v27_detector_prepare'->'request_sha'
 AND (t.state IN('failed','cancelled','skipped') OR stage.status IN('failed','cancelled','cancelling'))) INTO terminal;
 IF NOT terminal THEN RETURN false;END IF;
 END IF;
 UPDATE photo.runs SET v27_initial_recovery_token=NULL,v27_initial_recovery_until=NULL,v27_initial_recovery_done=terminal,
 v27_initial_recovery_failures=CASE WHEN p_outcome='retry' THEN least(r.v27_initial_recovery_failures::bigint+1,2147483647)::integer ELSE 0 END,
 v27_initial_recovery_after=CASE WHEN terminal THEN NULL WHEN p_outcome='retry' THEN clock_timestamp()+make_interval(secs=>least(300,5*power(2,least(r.v27_initial_recovery_failures,6)))::integer) ELSE clock_timestamp()+interval '5 seconds' END
 WHERE tenant_id=p_tenant AND id=p_detector;
 RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
