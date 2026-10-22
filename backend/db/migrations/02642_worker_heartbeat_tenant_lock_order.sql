-- +goose Up
-- Capacity/model-residency heartbeats retain node/registration locks until COMMIT.
-- Their deferred tenant FKs must follow the same tenant-root order as task writers.
-- Preserve the installed policy/model/backend/identity fences by guarded patches.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;signature text;
BEGIN
 signature:='photo.heartbeat_worker_capacity_v2(uuid,text,text,text[],bigint,bigint,bigint,text)';
 definition:=pg_get_functiondef(signature::regprocedure);
 needle:='DECLARE node text; nodes integer; accepted boolean;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'HEARTBEAT_TENANT_LOCK_DECLARATION_DRIFT';END IF;
 definition:=replace(definition,needle,needle||' tenant_scope uuid[]; locked_scope uuid[];');
 needle:=' PERFORM node_id FROM photo.resource_nodes WHERE node_id=node AND enabled FOR UPDATE;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'HEARTBEAT_TENANT_LOCK_NODE_DRIFT';END IF;
 replacement:=$patch$ -- 02642: authenticated tenant roots precede physical node/registration locks.
 SELECT array_agg(DISTINCT w.tenant_id ORDER BY w.tenant_id) INTO tenant_scope
 FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled;
 PERFORM t.id FROM photo.tenants t WHERE t.id=ANY(tenant_scope) ORDER BY t.id FOR UPDATE;
$patch$||needle;
 definition:=replace(definition,needle,replacement);
 needle:=' PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'HEARTBEAT_TENANT_LOCK_REGISTRATION_DRIFT';END IF;
 replacement:=needle||$patch$
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT array_agg(DISTINCT w.tenant_id ORDER BY w.tenant_id) INTO locked_scope
 FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled;
 IF locked_scope IS DISTINCT FROM tenant_scope THEN RETURN false;END IF;
$patch$;
 definition:=replace(definition,needle,replacement);
 EXECUTE definition;

 signature:='photo.report_worker_model_residency(uuid,text,jsonb)';
 definition:=pg_get_functiondef(signature::regprocedure);
 needle:='DECLARE item jsonb; n integer; updated integer;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'RESIDENCY_TENANT_LOCK_DECLARATION_DRIFT';END IF;
 definition:=replace(definition,needle,needle||' tenant_scope uuid[]; locked_scope uuid[];');
 needle:=' PERFORM 1 FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.enabled AND w.db_principal=session_user FOR UPDATE;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'RESIDENCY_TENANT_LOCK_REGISTRATION_DRIFT';END IF;
 replacement:=$patch$ -- 02642: standalone residency reports also acquire authenticated tenant roots.
 SELECT array_agg(DISTINCT w.tenant_id ORDER BY w.tenant_id) INTO tenant_scope
 FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled;
 PERFORM t.id FROM photo.tenants t WHERE t.id=ANY(tenant_scope) ORDER BY t.id FOR UPDATE;
$patch$||needle||$patch$
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT array_agg(DISTINCT w.tenant_id ORDER BY w.tenant_id) INTO locked_scope
 FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled;
 IF locked_scope IS DISTINCT FROM tenant_scope THEN RETURN false;END IF;
$patch$;
 definition:=replace(definition,needle,replacement);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- No ACL/owner/function signatures, task states, constraints or existing helpers change.
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
