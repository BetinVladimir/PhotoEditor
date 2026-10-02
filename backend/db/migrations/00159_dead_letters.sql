-- +goose Up
CREATE TABLE photo.system_admins(
 user_id uuid PRIMARY KEY REFERENCES photo.users(id) ON DELETE CASCADE,
 granted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 granted_by uuid REFERENCES photo.users(id),
 CHECK(granted_by IS NULL OR granted_by<>user_id)
);
REVOKE ALL ON photo.system_admins FROM PUBLIC;
GRANT SELECT ON photo.system_admins TO photo_api,photo_fn_owner;
GRANT SELECT ON photo.users,photo.sessions TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.authorize_system_admin(p_user uuid,p_session uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT p_user IS NOT NULL AND p_session IS NOT NULL AND EXISTS(
  SELECT FROM photo.system_admins a
  JOIN photo.users u ON u.id=a.user_id AND u.status='active'
  JOIN photo.sessions s ON s.user_id=u.id AND s.id=p_session AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
  WHERE a.user_id=p_user)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.authorize_system_admin(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_system_admin(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.authorize_system_admin(uuid,uuid) TO photo_api;

CREATE TABLE photo.dead_letters(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 tenant_id uuid REFERENCES photo.tenants(id),
 task_id uuid,
 event_id uuid,
 worker_class text NOT NULL CHECK(worker_class IN('cpu','gpu','media','archive','collection','mail','unknown')),
 routing_key text NOT NULL CHECK(length(routing_key) BETWEEN 1 AND 160),
 payload_sha256 text NOT NULL CHECK(payload_sha256 ~ '^[0-9a-f]{64}$'),
 metadata jsonb NOT NULL CHECK(jsonb_typeof(metadata)='object' AND octet_length(metadata::text)<=8192),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','replayed','dismissed')),
 seen_count bigint NOT NULL DEFAULT 1 CHECK(seen_count>0),
 last_seen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 replayed_at timestamptz,
 replayed_by uuid REFERENCES photo.users(id),
 UNIQUE(task_id,event_id,payload_sha256)
);
CREATE INDEX dead_letters_created ON photo.dead_letters(created_at,id);
ALTER TABLE photo.dead_letters ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.dead_letters FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.dead_letters TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.dead_letters FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE ON photo.dead_letters TO photo_fn_owner;

CREATE TABLE photo.admin_idempotency(
 user_id uuid NOT NULL REFERENCES photo.users(id),
 route text NOT NULL,
 key text NOT NULL,
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 response jsonb,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 PRIMARY KEY(user_id,route,key)
);
REVOKE ALL ON photo.admin_idempotency FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE ON photo.admin_idempotency TO photo_fn_owner;

-- The collector supplies only parsed identifiers and bounded broker metadata.
-- Raw message bytes are represented by their digest and are never persisted.
-- +goose StatementBegin
CREATE FUNCTION photo.record_dead_letter(p_task uuid,p_event uuid,p_class text,p_routing text,p_sha text,p_metadata jsonb)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant uuid;found uuid;
BEGIN
 IF p_class NOT IN('cpu','gpu','media','archive','collection','mail','unknown') OR p_routing IS NULL OR length(p_routing) NOT BETWEEN 1 AND 160
 OR p_sha !~ '^[0-9a-f]{64}$' OR p_metadata IS NULL OR jsonb_typeof(p_metadata)<>'object' OR octet_length(p_metadata::text)>8192
 OR p_metadata ?| ARRAY['payload','body','token','secret','authorization','cookie'] THEN RETURN NULL;END IF;
 IF p_task IS NOT NULL THEN SELECT tenant_id INTO tenant FROM photo.tasks WHERE id=p_task;END IF;
 INSERT INTO photo.dead_letters(tenant_id,task_id,event_id,worker_class,routing_key,payload_sha256,metadata)
 VALUES(tenant,p_task,p_event,p_class,p_routing,p_sha,p_metadata)
 ON CONFLICT(task_id,event_id,payload_sha256) DO UPDATE SET seen_count=photo.dead_letters.seen_count+1,last_seen_at=clock_timestamp()
 RETURNING id INTO found;
 RETURN found;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.record_dead_letter(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_dead_letter(uuid,uuid,text,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_dead_letter(uuid,uuid,text,text,text,jsonb) TO photo_publisher;

-- +goose StatementBegin
CREATE FUNCTION photo.list_dead_letters(p_user uuid,p_after_time timestamptz,p_after_id uuid,p_limit int)
RETURNS TABLE(id uuid,created_at timestamptz,tenant_id uuid,task_id uuid,event_id uuid,worker_class text,routing_key text,payload_sha256 text,metadata jsonb,state text,seen_count bigint,last_seen_at timestamptz,replayed_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_user IS NULL OR p_limit NOT BETWEEN 1 AND 101 OR (p_after_time IS NULL)<>(p_after_id IS NULL) OR NOT EXISTS(SELECT FROM photo.system_admins a WHERE a.user_id=p_user) THEN RETURN;END IF;
 RETURN QUERY SELECT d.id,d.created_at,d.tenant_id,d.task_id,d.event_id,d.worker_class,d.routing_key,d.payload_sha256,d.metadata,d.state,d.seen_count,d.last_seen_at,d.replayed_at
 FROM photo.dead_letters d WHERE p_after_time IS NULL OR (d.created_at,d.id)>(p_after_time,p_after_id) ORDER BY d.created_at,d.id LIMIT p_limit;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.list_dead_letters(uuid,timestamptz,uuid,int) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_dead_letters(uuid,timestamptz,uuid,int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.list_dead_letters(uuid,timestamptz,uuid,int) TO photo_api;

-- Replay never accepts a payload. It only makes the exact failed durable task
-- eligible for normal scheduler admission and dispatch with a new event/fence.
-- +goose StatementBegin
CREATE FUNCTION photo.replay_dead_letter(p_user uuid,p_letter uuid,p_key text,p_request_hash text)
RETURNS TABLE(outcome text,task_id uuid,status text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.dead_letters%ROWTYPE;t photo.tasks%ROWTYPE;idem photo.admin_idempotency%ROWTYPE;
BEGIN
 IF p_user IS NULL OR p_letter IS NULL OR p_key !~ '^[!-~]{1,128}$' OR p_request_hash !~ '^[0-9a-f]{64}$' OR NOT EXISTS(SELECT FROM photo.system_admins a WHERE a.user_id=p_user) THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::text;RETURN;END IF;
 INSERT INTO photo.admin_idempotency(user_id,route,key,request_hash,expires_at) VALUES(p_user,'Admin.DeadLetter.Replay',p_key,p_request_hash,clock_timestamp()+interval '30 days') ON CONFLICT DO NOTHING;
 SELECT * INTO idem FROM photo.admin_idempotency x WHERE x.user_id=p_user AND x.route='Admin.DeadLetter.Replay' AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text;RETURN;END IF;
 IF idem.response IS NOT NULL THEN RETURN QUERY SELECT 'replayed',(idem.response->>'task_id')::uuid,idem.response->>'status';RETURN;END IF;
 SELECT * INTO d FROM photo.dead_letters x WHERE x.id=p_letter FOR UPDATE;
 IF NOT FOUND OR d.task_id IS NULL THEN RETURN QUERY SELECT 'not_found',NULL::uuid,NULL::text;RETURN;END IF;
 SELECT * INTO t FROM photo.tasks x WHERE x.id=d.task_id FOR UPDATE;
 IF NOT FOUND OR t.state<>'failed' OR t.cancel_requested OR (t.lease_until IS NOT NULL AND t.lease_until>clock_timestamp()) OR t.canonical_result_id IS NOT NULL THEN RETURN QUERY SELECT 'not_replayable',NULL::uuid,NULL::text;RETURN;END IF;
 UPDATE photo.tasks SET state='ready',dispatch_command=NULL,worker_id=NULL,lease_until=NULL,next_attempt_at=clock_timestamp(),terminal_reason=NULL WHERE id=t.id;
 UPDATE photo.dead_letters SET state='replayed',replayed_at=clock_timestamp(),replayed_by=p_user WHERE id=d.id;
 UPDATE photo.admin_idempotency SET response=jsonb_build_object('task_id',t.id,'status','queued') WHERE user_id=p_user AND route='Admin.DeadLetter.Replay' AND key=p_key;
 RETURN QUERY SELECT 'accepted',t.id,'queued';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.replay_dead_letter(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.replay_dead_letter(uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.replay_dead_letter(uuid,uuid,text,text) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
