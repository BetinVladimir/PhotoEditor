-- +goose Up
-- Protect current user/session/membership/ACL rows through receipt transaction.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;locks text;
BEGIN
 SELECT pg_get_functiondef('photo.lock_v30_watermark_stage_source(uuid,uuid,integer)'::regprocedure) INTO definition;
 needle:='-- Repeat complete current readiness after all source locks, held until commit.';
 locks:=$patch$
 -- Lock the exact frozen prepare authority document before reading its session.
 PERFORM d.id FROM photo.backend_documents d WHERE d.tenant_id=e.tenant_id AND d.run_id=e.run_id
 AND d.owner_id=e.parent_task_id AND d.purpose='task_params' AND d.state='ready' AND d.v30_delivery_proof IS NOT NULL FOR SHARE NOWAIT;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM session_row.id FROM photo.backend_documents d
 JOIN photo.users actor ON actor.id=(body->'scope'->>'actor_id')::uuid
 JOIN photo.sessions session_row ON session_row.user_id=actor.id AND session_row.id=(d.v30_delivery_proof->>'auth_session_id')::uuid
 JOIN photo.memberships member ON member.tenant_id=e.tenant_id AND member.user_id=actor.id
 WHERE d.tenant_id=e.tenant_id AND d.run_id=e.run_id AND d.owner_id=e.parent_task_id AND d.purpose='task_params' AND d.state='ready'
 AND actor.status='active' AND member.status='active' AND session_row.revoked_at IS NULL AND session_row.expires_at>clock_timestamp()
 FOR SHARE OF actor,session_row,member NOWAIT;
 IF NOT FOUND THEN RETURN false;END IF;
 -- Missing ACL is allowed only if the repeated authorization predicate accepts
 -- workspace visibility or an owner/admin membership; project is already locked.
 PERFORM acl.id FROM photo.project_acl acl WHERE acl.tenant_id=e.tenant_id
 AND acl.project_id=(body->'scope'->>'project_id')::uuid AND acl.user_id=(body->'scope'->>'actor_id')::uuid FOR SHARE NOWAIT;
$patch$;
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark authority lock patch mismatch';END IF;
 EXECUTE replace(definition,needle,locks||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
