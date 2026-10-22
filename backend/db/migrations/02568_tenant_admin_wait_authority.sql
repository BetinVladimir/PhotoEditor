-- +goose Up
-- +goose StatementBegin
DO $migration$
DECLARE signature text; definition text; anchor text; replacement text;
BEGIN
 signature:='photo.create_invitation(uuid,uuid,uuid,uuid,text,text,bytea,timestamp with time zone,uuid,bytea,bytea,text,text,uuid,text,text)';
 IF NOT EXISTS(SELECT FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner JOIN pg_language l ON l.oid=p.prolang WHERE p.oid=to_regprocedure(signature) AND r.rolname='photo_fn_owner' AND p.prosecdef AND l.lanname='plpgsql') THEN RAISE EXCEPTION 'TENANT_INVITE_INSTALLATION_MISMATCH'; END IF;
 definition:=pg_get_functiondef(to_regprocedure(signature));
 anchor:=' IF idem.request_hash<>p_request_hash THEN';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'TENANT_INVITE_WAIT_ANCHOR_MISMATCH';END IF;
 replacement:=$patch$ -- tenant_admin_wait_authority_v1: receipt lock may outlive authority.
 IF NOT EXISTS(SELECT FROM photo.authorize_tenant(p_user,p_session,p_tenant) a WHERE a.role IN ('owner','admin')) THEN RETURN QUERY SELECT 'not_found',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
$patch$||anchor;
 definition:=replace(definition,anchor,replacement);
 anchor:=' IF idem.resource_id IS NOT NULL THEN SELECT * INTO i FROM photo.invitations WHERE tenant_id=p_tenant AND id=idem.resource_id;RETURN QUERY SELECT ''replayed'',i.id,i.email_normalized,i.role,i.expires_at;RETURN;END IF;';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'TENANT_INVITE_REPLAY_ANCHOR_MISMATCH';END IF;
 replacement:=$patch$ IF idem.resource_id IS NOT NULL THEN
 SELECT x.* INTO i FROM photo.invitations x WHERE x.tenant_id=p_tenant AND x.id=idem.resource_id AND x.invited_by=p_user FOR SHARE;
 IF NOT EXISTS(SELECT FROM photo.authorize_tenant(p_user,p_session,p_tenant) a WHERE a.role IN ('owner','admin')) THEN RETURN QUERY SELECT 'not_found',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 IF i.id IS NULL OR idem.response_status IS DISTINCT FROM 201 OR idem.response_body IS DISTINCT FROM jsonb_build_object('id',i.id) THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 RETURN QUERY SELECT 'replayed',i.id,i.email_normalized,i.role,i.expires_at;RETURN;END IF;$patch$;
 definition:=replace(definition,anchor,replacement);
 anchor:=' RETURN QUERY SELECT ''created'',i.id,i.email_normalized,i.role,i.expires_at;';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'TENANT_INVITE_FINAL_ANCHOR_MISMATCH';END IF;
 replacement:=$patch$ IF NOT EXISTS(SELECT FROM photo.authorize_tenant(p_user,p_session,p_tenant) a WHERE a.role IN ('owner','admin')) THEN RAISE EXCEPTION 'TENANT_ADMIN_AUTHORITY_CHANGED' USING ERRCODE='42501';END IF;
$patch$||anchor;
 EXECUTE replace(definition,anchor,replacement);
 signature:='photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid)';
 IF NOT EXISTS(SELECT FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner JOIN pg_language l ON l.oid=p.prolang WHERE p.oid=to_regprocedure(signature) AND r.rolname='photo_fn_owner' AND p.prosecdef AND l.lanname='plpgsql') THEN RAISE EXCEPTION 'TENANT_DELETE_INSTALLATION_MISMATCH'; END IF;
 definition:=pg_get_functiondef(to_regprocedure(signature));
 anchor:=' IF t.status=''deleting'' THEN';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'TENANT_DELETE_WAIT_ANCHOR_MISMATCH';END IF;
 replacement:=$patch$ -- tenant_admin_wait_authority_v1: re-read after the tenant lock.
 IF NOT EXISTS(SELECT FROM photo.memberships m JOIN photo.sessions s ON s.user_id=m.user_id JOIN photo.users u ON u.id=s.user_id WHERE m.tenant_id=p_tenant AND m.user_id=p_user AND m.role='owner' AND m.status='active' AND s.id=p_session AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND u.status='active') THEN RETURN QUERY SELECT 'not_found',NULL::uuid;RETURN;END IF;
$patch$||anchor;
 definition:=replace(definition,anchor,replacement);
 -- Both success paths may wait on cancellation/share/job rows. Refusal after
 -- writes must raise, rolling back the whole statement, not commit side effects.
 FOREACH anchor IN ARRAY ARRAY['RETURN QUERY SELECT ''deleting'',existing;','RETURN QUERY SELECT ''queued'',p_job;'] LOOP
  IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'TENANT_DELETE_FINAL_ANCHOR_MISMATCH';END IF;
  replacement:=$patch$IF NOT EXISTS(SELECT FROM photo.memberships m JOIN photo.sessions s ON s.user_id=m.user_id JOIN photo.users u ON u.id=s.user_id WHERE m.tenant_id=p_tenant AND m.user_id=p_user AND m.role='owner' AND m.status='active' AND s.id=p_session AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND u.status='active') THEN RAISE EXCEPTION 'TENANT_ADMIN_AUTHORITY_CHANGED' USING ERRCODE='42501';END IF;
$patch$||anchor;
  definition:=replace(definition,anchor,replacement);
 END LOOP;
 EXECUTE definition;
END $migration$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.read_invitation_receipt(p_user uuid,p_session uuid,p_tenant uuid,p_key text,p_hash text)
RETURNS TABLE(outcome text,invitation_id uuid,email_normalized text,role text,expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE;i photo.invitations%ROWTYPE;
BEGIN
 IF p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$' OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'INVALID_INVITATION_RECEIPT' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT FROM photo.authorize_tenant(p_user,p_session,p_tenant) a WHERE a.role IN ('owner','admin')) THEN RETURN QUERY SELECT 'not_found',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 SELECT x.* INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=p_tenant AND x.principal_type='user' AND x.principal_id=p_user AND x.route='tenant.invite' AND x.key=p_key FOR UPDATE;
 IF NOT EXISTS(SELECT FROM photo.authorize_tenant(p_user,p_session,p_tenant) a WHERE a.role IN ('owner','admin')) THEN RETURN QUERY SELECT 'not_found',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 IF idem.id IS NULL THEN RETURN QUERY SELECT 'missing',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 IF idem.request_hash<>p_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 IF idem.resource_id IS NULL AND idem.response_status IS NULL THEN RETURN QUERY SELECT 'missing',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 SELECT x.* INTO i FROM photo.invitations x WHERE x.tenant_id=p_tenant AND x.id=idem.resource_id AND x.invited_by=p_user FOR SHARE;
 IF i.id IS NULL OR idem.response_status IS DISTINCT FROM 201 OR idem.response_body IS DISTINCT FROM jsonb_build_object('id',i.id) THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 IF NOT EXISTS(SELECT FROM photo.authorize_tenant(p_user,p_session,p_tenant) a WHERE a.role IN ('owner','admin')) THEN RETURN QUERY SELECT 'not_found',NULL::uuid,NULL::text,NULL::text,NULL::timestamptz;RETURN;END IF;
 RETURN QUERY SELECT 'replayed',i.id,i.email_normalized,i.role,i.expires_at;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_invitation_receipt(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_invitation_receipt(uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_invitation_receipt(uuid,uuid,uuid,text,text) TO photo_api;
-- Default privileges may include unrelated application roles. Keep this
-- receipt reader API-only rather than inheriting deployment-local defaults.
-- +goose StatementBegin
DO $$ DECLARE grant_row record; BEGIN
 FOR grant_row IN SELECT DISTINCT a.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid='photo.read_invitation_receipt(uuid,uuid,uuid,text,text)'::regprocedure AND a.privilege_type='EXECUTE' AND a.grantee NOT IN(p.proowner,'photo_api'::regrole) LOOP
  IF grant_row.grantee=0 THEN EXECUTE 'REVOKE ALL ON FUNCTION photo.read_invitation_receipt(uuid,uuid,uuid,text,text) FROM PUBLIC';
  ELSE EXECUTE format('REVOKE ALL ON FUNCTION photo.read_invitation_receipt(uuid,uuid,uuid,text,text) FROM %I',pg_get_userbyid(grant_row.grantee));END IF;
 END LOOP;
END $$;
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
